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
	"encoding/json"
	"testing"
)

func TestDNSQueryTool_Name(t *testing.T) {
	tool := NewDNSQueryTool("")
	if tool.Name() != "dns.query" {
		t.Errorf("Name() = %q, want dns.query", tool.Name())
	}
}

func TestDNSQueryTool_Parameters(t *testing.T) {
	tool := NewDNSQueryTool("")
	params := tool.Parameters()
	if params == nil {
		t.Fatal("Parameters() returned nil")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties not found")
	}
	if _, ok := props["domain"]; !ok {
		t.Error("domain parameter not found")
	}
}

func TestDNSQueryTool_Execute_A(t *testing.T) {
	tool := NewDNSQueryTool("")
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]interface{}{
		"domain": "google.com",
		"type":   "A",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var dr dnsResult
	if err := json.Unmarshal([]byte(result), &dr); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if dr.Domain != "google.com" {
		t.Errorf("domain = %q, want google.com", dr.Domain)
	}
	if dr.Type != "A" {
		t.Errorf("type = %q, want A", dr.Type)
	}
	if len(dr.Records) == 0 && dr.Error == "" {
		t.Error("expected records or error")
	}
}

func TestDNSQueryTool_Execute_MX(t *testing.T) {
	tool := NewDNSQueryTool("")
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]interface{}{
		"domain": "google.com",
		"type":   "MX",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var dr dnsResult
	json.Unmarshal([]byte(result), &dr)
	if dr.Type != "MX" {
		t.Errorf("type = %q, want MX", dr.Type)
	}
}

func TestDNSQueryTool_Execute_TXT(t *testing.T) {
	tool := NewDNSQueryTool("")
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]interface{}{
		"domain": "google.com",
		"type":   "TXT",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var dr dnsResult
	json.Unmarshal([]byte(result), &dr)
	if dr.Type != "TXT" {
		t.Errorf("type = %q, want TXT", dr.Type)
	}
}

func TestDNSQueryTool_Execute_InvalidType(t *testing.T) {
	tool := NewDNSQueryTool("")
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]interface{}{
		"domain": "example.com",
		"type":   "INVALID",
	})
	if err == nil {
		t.Error("expected error for invalid record type")
	}
}

func TestDNSQueryTool_Execute_MissingDomain(t *testing.T) {
	tool := NewDNSQueryTool("")
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing domain")
	}
}

func TestDNSReverseTool_Name(t *testing.T) {
	tool := NewDNSReverseTool("")
	if tool.Name() != "dns.reverse" {
		t.Errorf("Name() = %q, want dns.reverse", tool.Name())
	}
}

func TestDNSReverseTool_Execute(t *testing.T) {
	tool := NewDNSReverseTool("")
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]interface{}{
		"ip": "8.8.8.8",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var dr dnsResult
	json.Unmarshal([]byte(result), &dr)
	if dr.Type != "PTR" {
		t.Errorf("type = %q, want PTR", dr.Type)
	}
	if dr.Domain != "8.8.8.8" {
		t.Errorf("domain = %q, want 8.8.8.8", dr.Domain)
	}
}

func TestDNSQueryTool_Capability(t *testing.T) {
	tool := NewDNSQueryTool("")
	cap := tool.Capability()
	if cap.Domain != "dns" {
		t.Errorf("domain = %q, want dns", cap.Domain)
	}
	if len(cap.SupportedTiers) != 1 || cap.SupportedTiers[0] != TierRead {
		t.Error("expected SupportedTiers = [TierRead]")
	}
}

func TestDNSQueryTool_ClassifyAction(t *testing.T) {
	tool := NewDNSQueryTool("")
	c := tool.ClassifyAction(map[string]interface{}{"domain": "example.com"})
	if c.Tier != TierRead {
		t.Errorf("tier = %d, want TierRead", c.Tier)
	}
	if c.Blocked {
		t.Error("DNS queries should never be blocked by classification")
	}
}
