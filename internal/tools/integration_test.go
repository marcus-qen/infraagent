/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package tools

import (
	"context"
	"testing"
)

// mockTool implements Tool for testing.
type mockTool struct {
	name string
	desc string
}

func (m *mockTool) Name() string                         { return m.name }
func (m *mockTool) Description() string                  { return m.desc }
func (m *mockTool) Parameters() map[string]interface{}    { return map[string]interface{}{"type": "object"} }
func (m *mockTool) Execute(_ context.Context, _ map[string]interface{}) (string, error) {
	return "ok", nil
}

// TestRegistryAcceptsAllToolTypes verifies the Registry can hold tools
// from all domains including state and A2A (which implement the Tool interface).
func TestRegistryAcceptsAllToolTypes(t *testing.T) {
	reg := NewRegistry()

	// Verify registry starts empty
	if len(reg.Definitions()) != 0 {
		t.Fatalf("expected empty registry, got %d tools", len(reg.Definitions()))
	}

	// Register a mock tool (simulating state/A2A tools)
	mock := &mockTool{name: "state_get", desc: "Read agent state"}
	reg.Register(mock)

	mock2 := &mockTool{name: "a2a_delegate", desc: "Delegate task"}
	reg.Register(mock2)

	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}

	// Verify tool names (sanitized: dots→underscores for LLM compatibility)
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}

	if !names["state_get"] {
		t.Error("missing state_get tool")
	}
	if !names["a2a_delegate"] {
		t.Error("missing a2a_delegate tool")
	}
}
