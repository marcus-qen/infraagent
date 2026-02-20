package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAgentYAML_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	os.WriteFile(path, []byte(generateAgentYAML("test", "agents", "Test", "observe", "*/5 * * * *", "standard", "kubernetes")), 0644)

	errors, warnings := validateAgentYAML(path)
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
	// Some warnings expected (e.g., no approval mode)
	_ = warnings
}

func TestValidateAgentYAML_BadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	os.WriteFile(path, []byte("{{invalid yaml"), 0644)

	errors, _ := validateAgentYAML(path)
	if errors == 0 {
		t.Error("expected errors for invalid YAML")
	}
}

func TestValidateAgentYAML_MissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	os.WriteFile(path, []byte(`apiVersion: legator.io/v1alpha1
kind: LegatorAgent
metadata:
  name: test
`), 0644)

	errors, _ := validateAgentYAML(path)
	if errors == 0 {
		t.Error("expected errors for missing spec")
	}
}

func TestValidateAgentYAML_BadAutonomy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	os.WriteFile(path, []byte(`apiVersion: legator.io/v1alpha1
kind: LegatorAgent
metadata:
  name: test
  namespace: agents
spec:
  autonomy: yolo
  modelTier: standard
  schedule:
    cron: "*/5 * * * *"
  skill:
    source: configmap://test
  guardrails:
    allowedTools:
      - kubectl.get
`), 0644)

	errors, _ := validateAgentYAML(path)
	if errors == 0 {
		t.Error("expected error for invalid autonomy level")
	}
}

func TestValidateEnvironmentYAML_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "environment.yaml")
	os.WriteFile(path, []byte(generateEnvironmentYAML("test", "agents", "kubernetes")), 0644)

	errors, _ := validateEnvironmentYAML(path)
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
}

func TestValidateSkill_Valid(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skill")
	os.MkdirAll(skillDir, 0755)

	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(generateSkillMD("cluster-health", "test", "kubernetes")), 0644)
	os.WriteFile(filepath.Join(skillDir, "actions.yaml"), []byte(generateActionsYAML("cluster-health", "kubernetes")), 0644)

	errors, _ := validateSkill(skillDir)
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
}

func TestValidateSkill_MissingSkillMD(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skill")
	os.MkdirAll(skillDir, 0755)

	errors, _ := validateSkill(skillDir)
	if errors == 0 {
		t.Error("expected error for missing SKILL.md")
	}
}

func TestValidateSkill_ShortSkillMD(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Tiny"), 0644)

	_, warnings := validateSkill(skillDir)
	if warnings == 0 {
		t.Error("expected warning for very short SKILL.md")
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		input interface{}
		want  int
	}{
		{42, 42},
		{float64(100), 100},
		{int64(200), 200},
		{"string", 0},
		{nil, 0},
	}
	for _, tt := range tests {
		got := toInt(tt.input)
		if got != tt.want {
			t.Errorf("toInt(%v) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
