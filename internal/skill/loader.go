/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package skill

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/marcus-qen/legator/internal/skills"
)

// Loader loads skills from various sources.
type Loader struct {
	client    client.Client
	namespace string
	cache     *Cache
}

// NewLoader creates a new skill loader.
func NewLoader(c client.Client, namespace string) *Loader {
	return &Loader{client: c, namespace: namespace}
}

// NewLoaderWithCache creates a skill loader with caching enabled.
func NewLoaderWithCache(c client.Client, namespace string, cache *Cache) *Loader {
	return &Loader{client: c, namespace: namespace, cache: cache}
}

// SetCache sets the cache for the loader.
func (l *Loader) SetCache(cache *Cache) {
	l.cache = cache
}

// Load loads a skill from the given source string.
// Source formats:
//   - "bundled" — load from bundled skills (embedded in controller)
//   - "configmap://name" or "configmap://name/key" — load from ConfigMap
//   - "git://github.com/org/repo#path@ref" — load from Git
//   - "oci://registry/repo:tag" — load from OCI registry via ORAS
func (l *Loader) Load(ctx context.Context, name, source string) (*Skill, error) {
	switch {
	case source == "bundled":
		return l.loadBundled(name)
	case strings.HasPrefix(source, "configmap://"):
		return l.loadFromConfigMap(ctx, name, source)
	case strings.HasPrefix(source, "git://"):
		return l.loadFromGit(ctx, name, source)
	case strings.HasPrefix(source, "oci://"):
		return l.loadFromOCI(ctx, name, source)
	default:
		// Also handle bare registry refs (host/repo:tag without oci:// prefix)
		if strings.Contains(source, "/") && (strings.Contains(source, ":") || strings.Contains(source, "@")) {
			return l.loadFromOCI(ctx, name, "oci://"+source)
		}
		// Try as ConfigMap name for backwards compat
		return l.loadFromConfigMap(ctx, name, "configmap://"+source)
	}
}

// loadBundled loads a skill from the bundled skill registry.
// In Phase 1, bundled skills return a stub that references the skill name.
// Real bundled skills will be embedded in the controller binary.
func (l *Loader) loadBundled(name string) (*Skill, error) {
	return &Skill{
		Name:         name,
		Description:  fmt.Sprintf("Bundled skill: %s", name),
		Version:      "bundled",
		Instructions: fmt.Sprintf("(Bundled skill '%s' — instructions loaded at runtime from embedded filesystem)", name),
	}, nil
}

// loadFromConfigMap loads a skill from a Kubernetes ConfigMap.
// The ConfigMap should have:
//   - key "SKILL.md" — the skill markdown with YAML frontmatter
//   - key "actions.yaml" (optional) — the Action Sheet
func (l *Loader) loadFromConfigMap(ctx context.Context, name, source string) (*Skill, error) {
	// Parse "configmap://name" or "configmap://name/key"
	cmRef := strings.TrimPrefix(source, "configmap://")
	cmName := cmRef
	mdKey := "SKILL.md"
	if idx := strings.Index(cmRef, "/"); idx > 0 {
		cmName = cmRef[:idx]
		mdKey = cmRef[idx+1:]
	}

	cm := &corev1.ConfigMap{}
	if err := l.client.Get(ctx, types.NamespacedName{
		Name:      cmName,
		Namespace: l.namespace,
	}, cm); err != nil {
		return nil, fmt.Errorf("failed to load ConfigMap %q: %w", cmName, err)
	}

	mdContent, ok := cm.Data[mdKey]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %q has no key %q", cmName, mdKey)
	}

	skill, err := Parse(mdContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse skill from ConfigMap %q: %w", cmName, err)
	}

	// Load actions.yaml if present
	if actionsYAML, ok := cm.Data["actions.yaml"]; ok {
		sheet, err := ParseActionSheet(actionsYAML)
		if err != nil {
			return nil, fmt.Errorf("failed to parse actions.yaml from ConfigMap %q: %w", cmName, err)
		}
		skill.Actions = sheet
	}

	return skill, nil
}

// loadFromOCI loads a skill from an OCI registry via ORAS.
// Source format: "oci://registry/repo:tag" or "oci://registry/repo@sha256:..."
func (l *Loader) loadFromOCI(ctx context.Context, name, source string) (*Skill, error) {
	refStr := strings.TrimPrefix(source, "oci://")

	ociRef, err := skills.ParseOCIRef(refStr)
	if err != nil {
		return nil, fmt.Errorf("invalid OCI reference %q: %w", refStr, err)
	}

	// Check cache first
	if l.cache != nil {
		if cached, ok := l.cache.Get(source); ok {
			return cached, nil
		}
	}

	// Build ORAS client with optional auth from env
	rc := skills.NewRegistryClient()
	if u := os.Getenv("LEGATOR_REGISTRY_USERNAME"); u != "" {
		rc.WithAuth(u, os.Getenv("LEGATOR_REGISTRY_PASSWORD"))
	}

	// Pull the skill content
	content, _, err := rc.Pull(ctx, ociRef)
	if err != nil {
		return nil, fmt.Errorf("pull OCI skill %q: %w", refStr, err)
	}

	// The content is the raw SKILL.md (pulled as tar.gz, extracted by PullToDir,
	// but Pull returns the raw content layer — we need to extract SKILL.md).
	// For now, use PullToDir to a temp dir then parse.
	tmpDir, err := os.MkdirTemp("", "legator-skill-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// If content looks like SKILL.md directly (starts with ---), parse it
	contentStr := string(content)
	if strings.HasPrefix(strings.TrimSpace(contentStr), "---") || strings.HasPrefix(strings.TrimSpace(contentStr), "name:") {
		skill, err := Parse(contentStr)
		if err != nil {
			return nil, fmt.Errorf("parse OCI skill %q: %w", refStr, err)
		}
		if skill.Name == "" {
			skill.Name = name
		}

		// Cache the result
		if l.cache != nil {
			l.cache.Put(source, skill)
		}
		return skill, nil
	}

	// Otherwise try PullToDir (tar.gz content)
	_, err = rc.PullToDir(ctx, ociRef, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("pull OCI skill to dir %q: %w", refStr, err)
	}

	// Read SKILL.md from extracted directory
	mdPath := tmpDir + "/SKILL.md"
	mdBytes, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md from OCI artifact %q: %w", refStr, err)
	}

	skill, err := Parse(string(mdBytes))
	if err != nil {
		return nil, fmt.Errorf("parse OCI skill %q: %w", refStr, err)
	}
	if skill.Name == "" {
		skill.Name = name
	}

	// Load actions.yaml if present
	actionsPath := tmpDir + "/actions.yaml"
	if actionsBytes, err := os.ReadFile(actionsPath); err == nil {
		sheet, err := ParseActionSheet(string(actionsBytes))
		if err == nil {
			skill.Actions = sheet
		}
	}

	// Cache the result
	if l.cache != nil {
		l.cache.Put(source, skill)
	}

	return skill, nil
}

// Parse parses a SKILL.md string into a Skill struct.
// Expects YAML frontmatter between --- delimiters followed by markdown body.
func Parse(content string) (*Skill, error) {
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, err
	}

	skill := &Skill{
		Instructions: strings.TrimSpace(body),
	}

	if frontmatter != "" {
		var fm map[string]interface{}
		if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
			return nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
		}
		skill.RawFrontmatter = fm

		if v, ok := fm["name"].(string); ok {
			skill.Name = v
		}
		if v, ok := fm["description"].(string); ok {
			skill.Description = v
		}
		if v, ok := fm["version"].(string); ok {
			skill.Version = v
		}
		if v, ok := fm["license"].(string); ok {
			skill.License = v
		}
		if tags, ok := fm["tags"]; ok {
			if tagList, ok := tags.([]interface{}); ok {
				for _, t := range tagList {
					if s, ok := t.(string); ok {
						skill.Tags = append(skill.Tags, s)
					}
				}
			}
		}
	}

	return skill, nil
}

// ParseActionSheet parses an actions.yaml string into an ActionSheet.
func ParseActionSheet(content string) (*ActionSheet, error) {
	sheet := &ActionSheet{}
	if err := yaml.Unmarshal([]byte(content), sheet); err != nil {
		return nil, fmt.Errorf("invalid actions.yaml: %w", err)
	}
	return sheet, nil
}

// splitFrontmatter splits YAML frontmatter from markdown body.
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return "", content, nil
	}

	// Find the closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content, nil
	}

	frontmatter = strings.TrimSpace(rest[:idx])
	body = rest[idx+4:] // skip \n---
	return frontmatter, body, nil
}
