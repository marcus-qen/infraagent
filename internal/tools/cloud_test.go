/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package tools

import (
	"testing"
)

// --- AWS classification tests ---

func TestClassifyAWSAction_Read(t *testing.T) {
	tests := []struct {
		service string
		command string
	}{
		{"ec2", "describe-instances"},
		{"s3", "ls"},
		{"iam", "list-users"},
		{"rds", "describe-db-instances"},
		{"cloudwatch", "get-metric-data"},
		{"sts", "get-caller-identity"},
	}

	for _, tt := range tests {
		c := classifyAWSAction(tt.service, tt.command)
		if c.Tier != TierRead {
			t.Errorf("classifyAWSAction(%q, %q) = tier %d, want TierRead", tt.service, tt.command, c.Tier)
		}
	}
}

func TestClassifyAWSAction_ServiceMutation(t *testing.T) {
	tests := []struct {
		service string
		command string
	}{
		{"ec2", "start-instances"},
		{"ec2", "stop-instances"},
		{"ec2", "reboot-instances"},
		{"ecs", "update-service"},
		{"rds", "reboot-db-instance"},
	}

	for _, tt := range tests {
		c := classifyAWSAction(tt.service, tt.command)
		if c.Tier != TierServiceMutation {
			t.Errorf("classifyAWSAction(%q, %q) = tier %d, want TierServiceMutation", tt.service, tt.command, c.Tier)
		}
	}
}

func TestClassifyAWSAction_Destructive(t *testing.T) {
	tests := []struct {
		service string
		command string
	}{
		{"ec2", "terminate-instances"},
		{"iam", "delete-user"},
		{"iam", "delete-role"},
		{"rds", "delete-db-instance"},
		{"lambda", "delete-function"},
	}

	for _, tt := range tests {
		c := classifyAWSAction(tt.service, tt.command)
		if c.Tier != TierDestructiveMutation {
			t.Errorf("classifyAWSAction(%q, %q) = tier %d, want TierDestructiveMutation", tt.service, tt.command, c.Tier)
		}
	}
}

func TestClassifyAWSAction_DataMutation(t *testing.T) {
	tests := []struct {
		service string
		command string
	}{
		{"s3", "rb"},
		{"s3", "rm"},
		{"s3api", "delete-object"},
		{"dynamodb", "delete-table"},
		{"dynamodb", "delete-item"},
	}

	for _, tt := range tests {
		c := classifyAWSAction(tt.service, tt.command)
		if c.Tier != TierDataMutation {
			t.Errorf("classifyAWSAction(%q, %q) = tier %d, want TierDataMutation", tt.service, tt.command, c.Tier)
		}
		if !c.Blocked {
			t.Errorf("classifyAWSAction(%q, %q) should be blocked", tt.service, tt.command)
		}
	}
}

func TestAWSCLITool_Name(t *testing.T) {
	tool := NewAWSCLITool("eu-west-1", nil)
	if tool.Name() != "aws.cli" {
		t.Errorf("Name() = %q, want aws.cli", tool.Name())
	}
}

func TestAWSCLITool_Capability(t *testing.T) {
	tool := NewAWSCLITool("", nil)
	cap := tool.Capability()
	if cap.Domain != "aws" {
		t.Errorf("domain = %q, want aws", cap.Domain)
	}
	if !cap.RequiresCredentials {
		t.Error("AWS tool should require credentials")
	}
}

func TestAWSCLITool_ClassifyAction(t *testing.T) {
	tool := NewAWSCLITool("", nil)
	c := tool.ClassifyAction(map[string]interface{}{"service": "s3", "command": "rm"})
	if c.Tier != TierDataMutation || !c.Blocked {
		t.Error("s3 rm should be blocked data mutation")
	}
}

func TestDefaultAWSProtectionClass(t *testing.T) {
	pc := DefaultAWSProtectionClass()
	if pc.Name != "aws" {
		t.Errorf("name = %q, want aws", pc.Name)
	}
	if len(pc.Rules) == 0 {
		t.Error("expected protection rules")
	}
}

// --- Azure classification tests ---

func TestClassifyAzureAction_Read(t *testing.T) {
	tests := []struct {
		group   string
		command string
	}{
		{"vm", "list"},
		{"vm", "show"},
		{"storage", "account list"},
		{"network", "nsg list"},
		{"aks", "list"},
	}

	for _, tt := range tests {
		c := classifyAzureAction(tt.group, tt.command)
		if c.Tier != TierRead {
			t.Errorf("classifyAzureAction(%q, %q) = tier %d, want TierRead", tt.group, tt.command, c.Tier)
		}
	}
}

func TestClassifyAzureAction_ServiceMutation(t *testing.T) {
	tests := []struct {
		group   string
		command string
	}{
		{"vm", "start"},
		{"vm", "stop"},
		{"vm", "deallocate"},
		{"vm", "restart"},
		{"aks", "scale"},
		{"webapp", "restart"},
	}

	for _, tt := range tests {
		c := classifyAzureAction(tt.group, tt.command)
		if c.Tier != TierServiceMutation {
			t.Errorf("classifyAzureAction(%q, %q) = tier %d, want TierServiceMutation", tt.group, tt.command, c.Tier)
		}
	}
}

func TestClassifyAzureAction_Destructive(t *testing.T) {
	tests := []struct {
		group   string
		command string
	}{
		{"vm", "delete"},
		{"group", "delete"},
		{"aks", "delete"},
		{"keyvault", "delete"},
	}

	for _, tt := range tests {
		c := classifyAzureAction(tt.group, tt.command)
		if c.Tier != TierDestructiveMutation {
			t.Errorf("classifyAzureAction(%q, %q) = tier %d, want TierDestructiveMutation", tt.group, tt.command, c.Tier)
		}
	}
}

func TestClassifyAzureAction_DataMutation(t *testing.T) {
	tests := []struct {
		group   string
		command string
	}{
		{"storage", "account.delete"},
		{"storage", "blob.delete"},
		{"storage", "container.delete"},
		{"cosmosdb", "database.delete"},
		{"keyvault", "secret.delete"},
	}

	for _, tt := range tests {
		c := classifyAzureAction(tt.group, tt.command)
		if c.Tier != TierDataMutation {
			t.Errorf("classifyAzureAction(%q, %q) = tier %d, want TierDataMutation", tt.group, tt.command, c.Tier)
		}
		if !c.Blocked {
			t.Errorf("classifyAzureAction(%q, %q) should be blocked", tt.group, tt.command)
		}
	}
}

func TestAzureCLITool_Name(t *testing.T) {
	tool := NewAzureCLITool("sub-123", nil)
	if tool.Name() != "az.cli" {
		t.Errorf("Name() = %q, want az.cli", tool.Name())
	}
}

func TestAzureCLITool_Capability(t *testing.T) {
	tool := NewAzureCLITool("", nil)
	cap := tool.Capability()
	if cap.Domain != "azure" {
		t.Errorf("domain = %q, want azure", cap.Domain)
	}
	if !cap.RequiresCredentials {
		t.Error("Azure tool should require credentials")
	}
}

func TestAzureCLITool_ClassifyAction(t *testing.T) {
	tool := NewAzureCLITool("", nil)
	c := tool.ClassifyAction(map[string]interface{}{"group": "storage", "command": "blob.delete"})
	if c.Tier != TierDataMutation || !c.Blocked {
		t.Error("storage blob.delete should be blocked data mutation")
	}
}

func TestDefaultAzureProtectionClass(t *testing.T) {
	pc := DefaultAzureProtectionClass()
	if pc.Name != "azure" {
		t.Errorf("name = %q, want azure", pc.Name)
	}
	if len(pc.Rules) == 0 {
		t.Error("expected protection rules")
	}
}
