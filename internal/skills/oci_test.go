/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPack(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal skill
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Test Skill\n\nDoes testing things.\n\n## Budget\n3 calls."), 0644)
	os.WriteFile(filepath.Join(dir, "actions.yaml"), []byte("actions:\n  - name: test\n    tool: kubectl.get\n    tier: read"), 0644)

	result, err := Pack(dir)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	if result.Manifest.Description != "Test Skill" {
		t.Errorf("description = %q, want 'Test Skill'", result.Manifest.Description)
	}
	if len(result.Manifest.Files) != 2 {
		t.Errorf("files = %d, want 2", len(result.Manifest.Files))
	}
	if len(result.Config) == 0 {
		t.Error("config should not be empty")
	}
	if len(result.Content) == 0 {
		t.Error("content should not be empty")
	}
}

func TestPack_MissingSKILLMD(t *testing.T) {
	dir := t.TempDir()

	_, err := Pack(dir)
	if err == nil {
		t.Error("expected error for missing SKILL.md")
	}
}

func TestPack_NotADirectory(t *testing.T) {
	_, err := Pack("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestUnpack(t *testing.T) {
	// Pack then unpack
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# Round Trip\n\nTest content."), 0644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "sub", "data.txt"), []byte("nested file"), 0644)

	result, err := Pack(srcDir)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	destDir := t.TempDir()
	err = Unpack(result.Content, destDir)
	if err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	// Verify files
	data, err := os.ReadFile(filepath.Join(destDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if string(data) != "# Round Trip\n\nTest content." {
		t.Errorf("SKILL.md content = %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(destDir, "sub", "data.txt"))
	if err != nil {
		t.Fatalf("read sub/data.txt: %v", err)
	}
	if string(data) != "nested file" {
		t.Errorf("sub/data.txt content = %q", string(data))
	}
}

func TestParseOCIRef(t *testing.T) {
	tests := []struct {
		input    string
		registry string
		path     string
		tag      string
		digest   string
	}{
		{"oci://ghcr.io/legator-skills/cluster-health:v1.0", "ghcr.io", "legator-skills/cluster-health", "v1.0", ""},
		{"oci://ghcr.io/my-org/my-skill:latest", "ghcr.io", "my-org/my-skill", "latest", ""},
		{"oci://ghcr.io/my-org/my-skill", "ghcr.io", "my-org/my-skill", "latest", ""},
		{"ghcr.io/org/skill:v2.0", "ghcr.io", "org/skill", "v2.0", ""},
		{"oci://ghcr.io/org/skill@sha256:abc123", "ghcr.io", "org/skill", "", "sha256:abc123"},
	}

	for _, tt := range tests {
		ref, err := ParseOCIRef(tt.input)
		if err != nil {
			t.Errorf("ParseOCIRef(%q) error: %v", tt.input, err)
			continue
		}
		if ref.Registry != tt.registry {
			t.Errorf("ParseOCIRef(%q).Registry = %q, want %q", tt.input, ref.Registry, tt.registry)
		}
		if ref.Path != tt.path {
			t.Errorf("ParseOCIRef(%q).Path = %q, want %q", tt.input, ref.Path, tt.path)
		}
		if ref.Tag != tt.tag {
			t.Errorf("ParseOCIRef(%q).Tag = %q, want %q", tt.input, ref.Tag, tt.tag)
		}
		if ref.Digest != tt.digest {
			t.Errorf("ParseOCIRef(%q).Digest = %q, want %q", tt.input, ref.Digest, tt.digest)
		}
	}
}

func TestParseOCIRef_Invalid(t *testing.T) {
	_, err := ParseOCIRef("oci://")
	if err == nil {
		t.Error("expected error for empty ref")
	}

	_, err = ParseOCIRef("oci://justregistry")
	if err == nil {
		t.Error("expected error for ref without path")
	}
}

func TestOCIRef_String(t *testing.T) {
	ref := &OCIRef{Registry: "ghcr.io", Path: "org/skill", Tag: "v1.0"}
	if ref.String() != "oci://ghcr.io/org/skill:v1.0" {
		t.Errorf("String() = %q", ref.String())
	}

	ref = &OCIRef{Registry: "ghcr.io", Path: "org/skill", Digest: "sha256:abc"}
	if ref.String() != "oci://ghcr.io/org/skill@sha256:abc" {
		t.Errorf("String() = %q", ref.String())
	}
}

func TestCache_PutAndGet(t *testing.T) {
	// Pack a skill to get content
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# Cache Test\n\nContent."), 0644)
	result, _ := Pack(srcDir)

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir, 1*time.Hour)

	// Store
	dir, err := cache.Put("oci://ghcr.io/org/test:v1", result.Content)
	if err != nil {
		t.Fatalf("Put error: %v", err)
	}

	// Retrieve
	cachedDir, ok := cache.Get("oci://ghcr.io/org/test:v1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if cachedDir != dir {
		t.Errorf("cached dir = %q, want %q", cachedDir, dir)
	}

	// Verify content
	data, err := os.ReadFile(filepath.Join(cachedDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read cached SKILL.md: %v", err)
	}
	if string(data) != "# Cache Test\n\nContent." {
		t.Errorf("cached content = %q", string(data))
	}
}

func TestCache_Miss(t *testing.T) {
	cache := NewCache(t.TempDir(), 1*time.Hour)
	_, ok := cache.Get("oci://nonexistent")
	if ok {
		t.Error("expected cache miss")
	}
}

func TestCache_Expired(t *testing.T) {
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# Expire\n\nTest."), 0644)
	result, _ := Pack(srcDir)

	cache := NewCache(t.TempDir(), 1*time.Millisecond) // very short TTL
	cache.Put("oci://test", result.Content)

	time.Sleep(5 * time.Millisecond)

	_, ok := cache.Get("oci://test")
	if ok {
		t.Error("expected cache miss after TTL")
	}
}

func TestCache_Evict(t *testing.T) {
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# Evict\n\nTest."), 0644)
	result, _ := Pack(srcDir)

	cache := NewCache(t.TempDir(), 1*time.Millisecond)
	cache.Put("oci://a", result.Content)
	cache.Put("oci://b", result.Content)

	time.Sleep(5 * time.Millisecond)

	evicted := cache.Evict()
	if evicted != 2 {
		t.Errorf("evicted = %d, want 2", evicted)
	}
	if cache.Size() != 0 {
		t.Errorf("size = %d, want 0", cache.Size())
	}
}

func TestCache_Size(t *testing.T) {
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# Size\n\nTest."), 0644)
	result, _ := Pack(srcDir)

	cache := NewCache(t.TempDir(), 1*time.Hour)
	if cache.Size() != 0 {
		t.Error("expected empty cache")
	}
	cache.Put("oci://a", result.Content)
	if cache.Size() != 1 {
		t.Errorf("size = %d, want 1", cache.Size())
	}
}
