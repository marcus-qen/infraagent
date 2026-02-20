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

// AWSCLITool wraps the AWS CLI for guardrailed cloud operations.
type AWSCLITool struct {
	region string
	// Credentials injected via environment variables (AWS_ACCESS_KEY_ID, etc.)
	// or via IRSA/Vault — never in args.
	env []string
}

// NewAWSCLITool creates an AWS CLI tool with optional region and env vars.
func NewAWSCLITool(region string, env []string) *AWSCLITool {
	return &AWSCLITool{region: region, env: env}
}

func (t *AWSCLITool) Name() string { return "aws.cli" }

func (t *AWSCLITool) Description() string {
	return "Execute AWS CLI commands. Read-only by default — mutations require appropriate autonomy level. Credentials are injected automatically."
}

func (t *AWSCLITool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"service": map[string]interface{}{
				"type":        "string",
				"description": "AWS service (e.g., ec2, s3, iam, rds, ecs, lambda, cloudwatch)",
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "AWS CLI command (e.g., 'describe-instances', 'list-buckets')",
			},
			"args": map[string]interface{}{
				"type":        "string",
				"description": "Additional arguments (e.g., '--instance-ids i-abc123 --output json')",
			},
		},
		"required": []string{"service", "command"},
	}
}

func (t *AWSCLITool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	service, _ := args["service"].(string)
	command, _ := args["command"].(string)
	extraArgs, _ := args["args"].(string)

	if service == "" || command == "" {
		return "", fmt.Errorf("service and command are required")
	}

	// Build command
	cmdArgs := []string{service, command}
	if t.region != "" {
		cmdArgs = append(cmdArgs, "--region", t.region)
	}
	cmdArgs = append(cmdArgs, "--output", "json")
	if extraArgs != "" {
		cmdArgs = append(cmdArgs, strings.Fields(extraArgs)...)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "aws", cmdArgs...)
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
func (t *AWSCLITool) Capability() ToolCapability {
	return ToolCapability{
		Domain:              "aws",
		SupportedTiers:      []ActionTier{TierRead, TierServiceMutation, TierDestructiveMutation},
		RequiresCredentials: true,
	}
}

// ClassifyAction classifies AWS CLI commands by risk tier.
func (t *AWSCLITool) ClassifyAction(args map[string]interface{}) ActionClassification {
	service, _ := args["service"].(string)
	command, _ := args["command"].(string)

	return classifyAWSAction(service, command)
}

// classifyAWSAction classifies an AWS service+command pair.
func classifyAWSAction(service, command string) ActionClassification {
	key := service + "." + command

	// Destructive mutations
	destructive := map[string]bool{
		"ec2.terminate-instances":      true,
		"ec2.delete-security-group":    true,
		"ec2.delete-vpc":              true,
		"ec2.delete-subnet":           true,
		"rds.delete-db-instance":      true,
		"rds.delete-db-cluster":       true,
		"iam.delete-user":             true,
		"iam.delete-role":             true,
		"iam.delete-policy":           true,
		"iam.put-user-policy":         true,
		"iam.attach-role-policy":      true,
		"iam.create-access-key":       true,
		"lambda.delete-function":      true,
		"ecs.delete-cluster":          true,
		"ecs.delete-service":          true,
		"cloudformation.delete-stack": true,
	}

	// Data mutations — ALWAYS blocked
	dataMutation := map[string]bool{
		"s3.rb":                       true, // remove bucket
		"s3.rm":                       true,
		"s3api.delete-bucket":         true,
		"s3api.delete-object":         true,
		"s3api.delete-objects":        true,
		"dynamodb.delete-table":       true,
		"dynamodb.delete-item":        true,
		"rds.delete-db-snapshot":      true,
		"rds.delete-db-cluster-snapshot": true,
	}

	// Service mutations
	serviceMutation := map[string]bool{
		"ec2.start-instances":         true,
		"ec2.stop-instances":          true,
		"ec2.reboot-instances":        true,
		"ec2.create-security-group":   true,
		"ec2.authorize-security-group-ingress": true,
		"ec2.revoke-security-group-ingress":    true,
		"ecs.update-service":          true,
		"lambda.update-function-code": true,
		"lambda.update-function-configuration": true,
		"rds.reboot-db-instance":      true,
		"rds.modify-db-instance":      true,
		"autoscaling.set-desired-capacity":     true,
		"autoscaling.update-auto-scaling-group": true,
	}

	if dataMutation[key] {
		return ActionClassification{
			Tier:        TierDataMutation,
			Blocked:     true,
			BlockReason: fmt.Sprintf("data mutation blocked: %s", key),
			Description: fmt.Sprintf("AWS %s (data mutation)", key),
		}
	}

	if destructive[key] {
		return ActionClassification{
			Tier:        TierDestructiveMutation,
			Description: fmt.Sprintf("AWS %s (destructive)", key),
		}
	}

	if serviceMutation[key] {
		return ActionClassification{
			Tier:        TierServiceMutation,
			Description: fmt.Sprintf("AWS %s (service mutation)", key),
		}
	}

	// Default: read
	return ActionClassification{
		Tier:        TierRead,
		Description: fmt.Sprintf("AWS %s (read)", key),
	}
}

// DefaultAWSProtectionClass returns the built-in AWS protection rules.
func DefaultAWSProtectionClass() ProtectionClass {
	return ProtectionClass{
		Name:        "aws",
		Description: "AWS cloud resource protection",
		Rules: []ProtectionRule{
			{Domain: "aws", Pattern: "s3.delete-*", Action: ProtectionBlock, Description: "S3 deletions blocked"},
			{Domain: "aws", Pattern: "s3.rb", Action: ProtectionBlock, Description: "S3 bucket removal blocked"},
			{Domain: "aws", Pattern: "s3.rm", Action: ProtectionBlock, Description: "S3 object removal blocked"},
			{Domain: "aws", Pattern: "dynamodb.delete-*", Action: ProtectionBlock, Description: "DynamoDB deletions blocked"},
			{Domain: "aws", Pattern: "iam.delete-*", Action: ProtectionAudit, Description: "IAM deletions audited"},
			{Domain: "aws", Pattern: "iam.create-access-key", Action: ProtectionApprove, Description: "IAM key creation requires approval"},
			{Domain: "aws", Pattern: "rds.delete-*", Action: ProtectionBlock, Description: "RDS deletions blocked"},
			{Domain: "aws", Pattern: "ec2.terminate-*", Action: ProtectionApprove, Description: "EC2 termination requires approval"},
		},
	}
}
