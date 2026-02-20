/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// AzureCLITool wraps the Azure CLI for guardrailed cloud operations.
type AzureCLITool struct {
	subscription string
	// Credentials injected via environment variables or managed identity.
	env []string
}

// NewAzureCLITool creates an Azure CLI tool with optional subscription and env vars.
func NewAzureCLITool(subscription string, env []string) *AzureCLITool {
	return &AzureCLITool{subscription: subscription, env: env}
}

func (t *AzureCLITool) Name() string { return "az.cli" }

func (t *AzureCLITool) Description() string {
	return "Execute Azure CLI commands. Read-only by default — mutations require appropriate autonomy level. Credentials are injected automatically."
}

func (t *AzureCLITool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"group": map[string]interface{}{
				"type":        "string",
				"description": "Azure CLI command group (e.g., vm, storage, network, aks, sql, keyvault)",
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Azure CLI command (e.g., 'list', 'show', 'deallocate')",
			},
			"args": map[string]interface{}{
				"type":        "string",
				"description": "Additional arguments (e.g., '--resource-group myRG --output json')",
			},
		},
		"required": []string{"group", "command"},
	}
}

func (t *AzureCLITool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	group, _ := args["group"].(string)
	command, _ := args["command"].(string)
	extraArgs, _ := args["args"].(string)

	if group == "" || command == "" {
		return "", fmt.Errorf("group and command are required")
	}

	// Build command
	cmdArgs := []string{group, command}
	if t.subscription != "" {
		cmdArgs = append(cmdArgs, "--subscription", t.subscription)
	}
	cmdArgs = append(cmdArgs, "--output", "json")
	if extraArgs != "" {
		cmdArgs = append(cmdArgs, strings.Fields(extraArgs)...)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "az", cmdArgs...)
	cmd.Env = t.env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if len(output) > 8192 {
		output = output[:8192] + "\n... (truncated)"
	}

	if err != nil {
		errMsg := stderr.String()
		if len(errMsg) > 2048 {
			errMsg = errMsg[:2048]
		}
		return fmt.Sprintf("Error: %s\n%s", err.Error(), errMsg), nil
	}

	return output, nil
}

// Capability implements ClassifiableTool.
func (t *AzureCLITool) Capability() ToolCapability {
	return ToolCapability{
		Domain:              "azure",
		SupportedTiers:      []ActionTier{TierRead, TierServiceMutation, TierDestructiveMutation},
		RequiresCredentials: true,
	}
}

// ClassifyAction classifies Azure CLI commands by risk tier.
func (t *AzureCLITool) ClassifyAction(args map[string]interface{}) ActionClassification {
	group, _ := args["group"].(string)
	command, _ := args["command"].(string)

	return classifyAzureAction(group, command)
}

// classifyAzureAction classifies an Azure group+command pair.
func classifyAzureAction(group, command string) ActionClassification {
	key := group + "." + command

	// Destructive mutations
	destructive := map[string]bool{
		"vm.delete":                     true,
		"group.delete":                  true, // resource group deletion
		"aks.delete":                    true,
		"network.vnet.delete":           true,
		"network.nsg.delete":            true,
		"sql.server.delete":             true,
		"sql.db.delete":                 true,
		"keyvault.delete":               true,
		"ad.sp.delete":                  true,  // service principal
		"role.assignment.delete":         true,
		"role.definition.delete":         true,
		"functionapp.delete":            true,
		"webapp.delete":                 true,
		"cosmosdb.delete":               true,
	}

	// Data mutations — ALWAYS blocked
	dataMutation := map[string]bool{
		"storage.account.delete":        true,
		"storage.blob.delete":           true,
		"storage.container.delete":      true,
		"sql.db.delete":                 true,
		"cosmosdb.collection.delete":    true,
		"cosmosdb.database.delete":      true,
		"keyvault.secret.delete":        true,
		"keyvault.key.delete":           true,
		"backup.vault.delete":           true,
	}

	// Service mutations
	serviceMutation := map[string]bool{
		"vm.start":                      true,
		"vm.stop":                       true,
		"vm.deallocate":                 true,
		"vm.restart":                    true,
		"vm.resize":                     true,
		"aks.scale":                     true,
		"aks.upgrade":                   true,
		"webapp.restart":                true,
		"functionapp.restart":           true,
		"network.nsg.rule.create":       true,
		"network.nsg.rule.delete":       true,
		"network.nsg.rule.update":       true,
		"sql.db.update":                 true,
		"role.assignment.create":        true,
		"ad.sp.create":                  true,
	}

	if dataMutation[key] {
		return ActionClassification{
			Tier:        TierDataMutation,
			Blocked:     true,
			BlockReason: fmt.Sprintf("data mutation blocked: %s", key),
			Description: fmt.Sprintf("Azure %s (data mutation)", key),
		}
	}

	if destructive[key] {
		return ActionClassification{
			Tier:        TierDestructiveMutation,
			Description: fmt.Sprintf("Azure %s (destructive)", key),
		}
	}

	if serviceMutation[key] {
		return ActionClassification{
			Tier:        TierServiceMutation,
			Description: fmt.Sprintf("Azure %s (service mutation)", key),
		}
	}

	// Default: read
	return ActionClassification{
		Tier:        TierRead,
		Description: fmt.Sprintf("Azure %s (read)", key),
	}
}

// DefaultAzureProtectionClass returns the built-in Azure protection rules.
func DefaultAzureProtectionClass() ProtectionClass {
	return ProtectionClass{
		Name:        "azure",
		Description: "Azure cloud resource protection",
		Rules: []ProtectionRule{
			{Domain: "azure", Pattern: "storage.*.delete", Action: ProtectionBlock, Description: "Storage deletions blocked"},
			{Domain: "azure", Pattern: "sql.db.delete", Action: ProtectionBlock, Description: "SQL database deletion blocked"},
			{Domain: "azure", Pattern: "cosmosdb.*.delete", Action: ProtectionBlock, Description: "CosmosDB deletions blocked"},
			{Domain: "azure", Pattern: "keyvault.*.delete", Action: ProtectionBlock, Description: "Key Vault deletions blocked"},
			{Domain: "azure", Pattern: "backup.*.delete", Action: ProtectionBlock, Description: "Backup deletions blocked"},
			{Domain: "azure", Pattern: "group.delete", Action: ProtectionApprove, Description: "Resource group deletion requires approval"},
			{Domain: "azure", Pattern: "vm.delete", Action: ProtectionApprove, Description: "VM deletion requires approval"},
			{Domain: "azure", Pattern: "aks.delete", Action: ProtectionApprove, Description: "AKS cluster deletion requires approval"},
			{Domain: "azure", Pattern: "role.*.delete", Action: ProtectionAudit, Description: "RBAC changes audited"},
		},
	}
}
