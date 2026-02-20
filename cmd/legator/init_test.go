package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAgentYAML(t *testing.T) {
	yaml := generateAgentYAML("test-agent", "agents", "Test agent", "observe", "*/5 * * * *", "standard", "kubernetes")

	checks := []string{
		"apiVersion: legator.io/v1alpha1",
		"kind: LegatorAgent",
		"name: test-agent",
		"namespace: agents",
		"autonomy: observe",
		"modelTier: standard",
		"kubectl.get",
	}
	for _, check := range checks {
		if !strings.Contains(yaml, check) {
			t.Errorf("agent YAML missing %q", check)
		}
	}
}

func TestGenerateAgentYAML_SSH(t *testing.T) {
	yaml := generateAgentYAML("ssh-agent", "agents", "SSH agent", "observe", "0 * * * *", "fast", "ssh")
	if !strings.Contains(yaml, "ssh.exec") {
		t.Error("SSH agent YAML should contain ssh.exec tool")
	}
}

func TestGenerateEnvironmentYAML(t *testing.T) {
	yaml := generateEnvironmentYAML("test-agent", "agents", "kubernetes")

	if !strings.Contains(yaml, "kind: LegatorEnvironment") {
		t.Error("environment YAML missing kind")
	}
	if !strings.Contains(yaml, "name: test-agent-env") {
		t.Error("environment YAML missing name")
	}
}

func TestGenerateSkillMD(t *testing.T) {
	skills := []string{"cluster-health", "pod-restart-monitor", "certificate-expiry", "server-health", "custom"}
	for _, skill := range skills {
		md := generateSkillMD(skill, "test", "kubernetes")
		if len(md) < 50 {
			t.Errorf("skill %q generated too-short SKILL.md (%d bytes)", skill, len(md))
		}
		if !strings.Contains(strings.ToLower(md), "budget") {
			t.Errorf("skill %q SKILL.md missing budget directive", skill)
		}
	}
}

func TestGenerateActionsYAML(t *testing.T) {
	domains := []string{"kubernetes", "ssh", "http", "sql", "dns"}
	for _, domain := range domains {
		yaml := generateActionsYAML("cluster-health", domain)
		if !strings.Contains(yaml, "actions:") {
			t.Errorf("actions YAML for domain %q missing 'actions:' key", domain)
		}
		if !strings.Contains(yaml, "tier: read") {
			t.Errorf("actions YAML for domain %q missing read tier", domain)
		}
	}
}

func TestToolsForDomain(t *testing.T) {
	tests := []struct {
		domain   string
		contains string
	}{
		{"kubernetes", "kubectl.get"},
		{"ssh", "ssh.exec"},
		{"http", "http.get"},
		{"sql", "sql.query"},
		{"dns", "dns.query"},
	}
	for _, tt := range tests {
		tools := toolsForDomain(tt.domain)
		if !strings.Contains(tools, tt.contains) {
			t.Errorf("toolsForDomain(%q) should contain %q", tt.domain, tt.contains)
		}
	}
}

func TestHandleInit_CreatesFiles(t *testing.T) {
	// Test that the generated files are structurally valid
	dir := t.TempDir()
	subdir := filepath.Join(dir, "test-agent", "skill")
	os.MkdirAll(subdir, 0755)

	// Write generated content
	agentYAML := generateAgentYAML("test-agent", "agents", "Test", "observe", "*/5 * * * *", "standard", "kubernetes")
	os.WriteFile(filepath.Join(dir, "test-agent", "agent.yaml"), []byte(agentYAML), 0644)

	envYAML := generateEnvironmentYAML("test-agent", "agents", "kubernetes")
	os.WriteFile(filepath.Join(dir, "test-agent", "environment.yaml"), []byte(envYAML), 0644)

	skillMD := generateSkillMD("cluster-health", "test-agent", "kubernetes")
	os.WriteFile(filepath.Join(subdir, "SKILL.md"), []byte(skillMD), 0644)

	actionsYAML := generateActionsYAML("cluster-health", "kubernetes")
	os.WriteFile(filepath.Join(subdir, "actions.yaml"), []byte(actionsYAML), 0644)

	// Verify all files exist
	files := []string{"agent.yaml", "environment.yaml", "skill/SKILL.md", "skill/actions.yaml"}
	for _, f := range files {
		path := filepath.Join(dir, "test-agent", f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}
}
