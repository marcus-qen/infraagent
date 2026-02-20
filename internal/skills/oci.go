/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package skills provides OCI-based skill distribution.
// Skills are packaged as OCI artifacts and pushed/pulled from container registries.
// Format: oci://registry/org/skill-name:version
//
// Skill artifact structure:
//   - SKILL.md (required) — agent expertise document
//   - actions.yaml (optional) — action sheet
//   - Additional assets (optional)
//
// Media types:
//   - application/vnd.legator.skill.config.v1+json (config)
//   - application/vnd.legator.skill.content.v1.tar+gzip (content layer)
package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// MediaTypeConfig is the OCI media type for skill config.
	MediaTypeConfig = "application/vnd.legator.skill.config.v1+json"
	// MediaTypeContent is the OCI media type for skill content layer.
	MediaTypeContent = "application/vnd.legator.skill.content.v1.tar+gzip"
)

// SkillManifest describes a packaged skill.
type SkillManifest struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Files       []string  `json:"files"`
	CreatedAt   time.Time `json:"createdAt"`
}

// PackResult holds the result of packaging a skill directory.
type PackResult struct {
	Manifest SkillManifest
	Config   []byte // JSON config blob
	Content  []byte // tar.gz content layer
}

// Pack packages a skill directory into OCI artifact layers.
func Pack(dir string) (*PackResult, error) {
	// Validate directory
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	// Check for SKILL.md
	skillPath := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("SKILL.md not found in %s", dir)
	}

	// Collect files
	var files []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	// Create tar.gz
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, file := range files {
		fullPath := filepath.Join(dir, file)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}

		header := &tar.Header{
			Name:    file,
			Size:    int64(len(data)),
			Mode:    0644,
			ModTime: time.Now(),
		}
		if err := tw.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("tar header %s: %w", file, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("tar write %s: %w", file, err)
		}
	}

	tw.Close()
	gw.Close()

	// Build manifest
	name := filepath.Base(dir)
	manifest := SkillManifest{
		Name:      name,
		Version:   "latest",
		Files:     files,
		CreatedAt: time.Now().UTC(),
	}

	// Read description from SKILL.md first line
	skillData, _ := os.ReadFile(skillPath)
	lines := strings.Split(string(skillData), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			manifest.Description = line
			break
		}
		if strings.HasPrefix(line, "# ") {
			manifest.Description = strings.TrimPrefix(line, "# ")
			break
		}
	}

	config, _ := json.MarshalIndent(manifest, "", "  ")

	return &PackResult{
		Manifest: manifest,
		Config:   config,
		Content:  buf.Bytes(),
	}, nil
}

// Unpack extracts a skill artifact into a directory.
func Unpack(content []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		// Security: prevent path traversal
		target := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
			return fmt.Errorf("path traversal detected: %s", header.Name)
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("create parent: %w", err)
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("read %s: %w", header.Name, err)
		}

		if err := os.WriteFile(target, data, os.FileMode(header.Mode)); err != nil {
			return fmt.Errorf("write %s: %w", header.Name, err)
		}
	}

	return nil
}

// --- Reference Parsing ---

// OCIRef represents a parsed OCI skill reference.
// Format: oci://registry/org/name:tag or oci://registry/org/name@sha256:digest
type OCIRef struct {
	Registry string
	Path     string // org/name
	Tag      string
	Digest   string
}

// ParseOCIRef parses an OCI skill reference string.
func ParseOCIRef(ref string) (*OCIRef, error) {
	ref = strings.TrimPrefix(ref, "oci://")
	if ref == "" {
		return nil, fmt.Errorf("empty OCI reference")
	}

	result := &OCIRef{}

	// Check for digest
	if idx := strings.Index(ref, "@sha256:"); idx >= 0 {
		result.Digest = ref[idx+1:]
		ref = ref[:idx]
	}

	// Check for tag
	// Find the last colon that isn't part of a port
	parts := strings.Split(ref, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid OCI reference: need at least registry/name")
	}

	// Last part may have :tag
	last := parts[len(parts)-1]
	if idx := strings.LastIndex(last, ":"); idx >= 0 {
		result.Tag = last[idx+1:]
		parts[len(parts)-1] = last[:idx]
	}

	if result.Tag == "" && result.Digest == "" {
		result.Tag = "latest"
	}

	result.Registry = parts[0]
	result.Path = strings.Join(parts[1:], "/")

	return result, nil
}

// String returns the full OCI reference string.
func (r *OCIRef) String() string {
	s := fmt.Sprintf("oci://%s/%s", r.Registry, r.Path)
	if r.Digest != "" {
		s += "@" + r.Digest
	} else if r.Tag != "" {
		s += ":" + r.Tag
	}
	return s
}

// --- Cache ---

// CachedSkill is a skill stored in the local cache.
type CachedSkill struct {
	Ref       string
	Dir       string
	FetchedAt time.Time
}

// Cache stores pulled skills locally with TTL-based expiry.
type Cache struct {
	mu       sync.RWMutex
	entries  map[string]CachedSkill
	baseDir  string
	ttl      time.Duration
}

// NewCache creates a skill cache.
func NewCache(baseDir string, ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]CachedSkill),
		baseDir: baseDir,
		ttl:     ttl,
	}
}

// Get returns a cached skill if it exists and hasn't expired.
func (c *Cache) Get(ref string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[ref]
	if !ok {
		return "", false
	}

	if time.Since(entry.FetchedAt) > c.ttl {
		return "", false // expired
	}

	return entry.Dir, true
}

// Put stores a skill in the cache.
func (c *Cache) Put(ref string, content []byte) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create cache directory
	safeName := strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(ref)
	dir := filepath.Join(c.baseDir, safeName)

	if err := Unpack(content, dir); err != nil {
		return "", fmt.Errorf("unpack to cache: %w", err)
	}

	c.entries[ref] = CachedSkill{
		Ref:       ref,
		Dir:       dir,
		FetchedAt: time.Now(),
	}

	return dir, nil
}

// Evict removes expired entries from the cache.
func (c *Cache) Evict() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	evicted := 0
	for ref, entry := range c.entries {
		if time.Since(entry.FetchedAt) > c.ttl {
			os.RemoveAll(entry.Dir)
			delete(c.entries, ref)
			evicted++
		}
	}
	return evicted
}

// Size returns the number of cached skills.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// --- Skill Loader (source: oci://) ---

// Loader fetches skills from OCI registries with caching.
type Loader struct {
	cache  *Cache
	// In a real implementation, this would use ORAS to push/pull.
	// For now we provide the Pack/Unpack primitives and the cache layer.
	// The actual OCI registry interaction will be wired when we add
	// the ORAS dependency.
}

// NewLoader creates an OCI skill loader.
func NewLoader(cache *Cache) *Loader {
	return &Loader{cache: cache}
}

// LoadFromCache loads a skill from the local cache, returning the directory path.
func (l *Loader) LoadFromCache(ref string) (string, bool) {
	return l.cache.Get(ref)
}

// StoreInCache stores skill content in the cache for the given reference.
func (l *Loader) StoreInCache(ref string, content []byte) (string, error) {
	return l.cache.Put(ref, content)
}

// PushSkill packages a local directory and returns the artifact layers.
// The actual registry push will use ORAS — this prepares the layers.
func PushSkill(ctx context.Context, dir string) (*PackResult, error) {
	return Pack(dir)
}
