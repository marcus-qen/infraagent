/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// handleInit implements the `legator init` wizard.
// Creates a minimal agent directory with agent.yaml, environment.yaml,
// and a starter skill.
func handleInit(args []string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🚀 Legator Agent Init")
	fmt.Println("=====================")
	fmt.Println()

	// Agent name
	name := prompt(reader, "Agent name", "my-agent")

	// Description
	description := prompt(reader, "Description", "Autonomous agent for monitoring and management")

	// Namespace
	namespace := prompt(reader, "Namespace", "agents")

	// Autonomy level
	fmt.Println()
	fmt.Println("Autonomy levels:")
	fmt.Println("  observe            — Read-only monitoring, reports findings")
	fmt.Println("  recommend          — Suggests actions, never executes")
	fmt.Println("  automate-safe      — Executes non-destructive mutations")
	fmt.Println("  automate-destructive — Full automation (use with caution)")
	autonomy := prompt(reader, "Autonomy level", "observe")

	// Schedule
	fmt.Println()
	fmt.Println("Schedule examples:")
	fmt.Println("  */5 * * * *     — every 5 minutes")
	fmt.Println("  0 * * * *       — hourly")
	fmt.Println("  0 8 * * *       — daily at 8am")
	fmt.Println("  0 0 * * 0       — weekly (Sunday midnight)")
	schedule := prompt(reader, "Schedule (cron)", "0 * * * *")

	// Model tier
	fmt.Println()
	fmt.Println("Model tiers (resolved to actual models by ModelTierConfig):")
	fmt.Println("  fast       — Quick checks, high-frequency monitoring")
	fmt.Println("  standard   — Most analysis and operational tasks")
	fmt.Println("  reasoning  — Complex planning, incident response")
	modelTier := prompt(reader, "Model tier", "standard")

	// Tool domain
	fmt.Println()
	fmt.Println("Tool domains:")
	fmt.Println("  kubernetes  — kubectl get/describe/logs/apply/scale/delete")
	fmt.Println("  ssh         — SSH into servers (ssh.exec)")
	fmt.Println("  http        — HTTP GET/POST/DELETE")
	fmt.Println("  sql         — SQL queries (read-only)")
	fmt.Println("  dns         — DNS lookups (dns.query, dns.reverse)")
	domain := prompt(reader, "Primary tool domain", "kubernetes")

	// Starter skill
	fmt.Println()
	fmt.Println("Starter skills (bundled templates):")
	fmt.Println("  cluster-health       — K8s pod/node monitoring, crash detection")
	fmt.Println("  pod-restart-monitor  — Watch for crash loops and OOMKills")
	fmt.Println("  certificate-expiry   — TLS certificate expiry checking")
	fmt.Println("  server-health        — SSH-based server monitoring")
	fmt.Println("  custom               — Empty skill template")
	skill := prompt(reader, "Starter skill", "cluster-health")

	// Create output directory
	dir := name
	if len(args) > 0 {
		dir = args[0]
	}

	fmt.Println()
	fmt.Printf("Creating agent in ./%s ...\n", dir)

	if err := os.MkdirAll(filepath.Join(dir, "skill"), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
		os.Exit(1)
	}

	// Write agent.yaml
	agentYAML := generateAgentYAML(name, namespace, description, autonomy, schedule, modelTier, domain)
	writeFile(filepath.Join(dir, "agent.yaml"), agentYAML)

	// Write environment.yaml
	envYAML := generateEnvironmentYAML(name, namespace, domain)
	writeFile(filepath.Join(dir, "environment.yaml"), envYAML)

	// Write skill
	skillMD := generateSkillMD(skill, name, domain)
	writeFile(filepath.Join(dir, "skill", "SKILL.md"), skillMD)

	// Write actions.yaml
	actionsYAML := generateActionsYAML(skill, domain)
	writeFile(filepath.Join(dir, "skill", "actions.yaml"), actionsYAML)

	fmt.Println()
	fmt.Println("✅ Agent scaffold created!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Review and edit %s/agent.yaml\n", dir)
	fmt.Printf("  2. Customize %s/skill/SKILL.md with your agent's expertise\n", dir)
	fmt.Printf("  3. Validate:  legator validate %s/\n", dir)
	fmt.Printf("  4. Apply:     kubectl apply -f %s/agent.yaml\n", dir)
	fmt.Printf("               kubectl apply -f %s/environment.yaml\n", dir)
	fmt.Printf("  5. Watch:     legator runs list --agent %s\n", name)
}

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	fmt.Printf("  %s [%s]: ", label, defaultVal)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func writeFile(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("  📄 %s\n", path)
}

func generateAgentYAML(name, namespace, description, autonomy, schedule, modelTier, domain string) string {
	tools := toolsForDomain(domain)
	return fmt.Sprintf(`apiVersion: legator.io/v1alpha1
kind: LegatorAgent
metadata:
  name: %s
  namespace: %s
spec:
  description: "%s"

  schedule:
    cron: "%s"

  modelTier: %s

  autonomy: %s

  guardrails:
    maxIterations: 10
    tokenBudget: 32000
    approvalMode: none
    allowedTools:
%s
  skill:
    source: configmap://%s-skill

  environmentRef:
    name: %s-env
`, name, namespace, description, schedule, modelTier, autonomy, tools, name, name)
}

func toolsForDomain(domain string) string {
	switch domain {
	case "kubernetes":
		return `      - kubectl.get
      - kubectl.describe
      - kubectl.logs`
	case "ssh":
		return `      - ssh.exec`
	case "http":
		return `      - http.get
      - http.post`
	case "sql":
		return `      - sql.query`
	case "dns":
		return `      - dns.query
      - dns.reverse`
	default:
		return `      - kubectl.get
      - kubectl.describe
      - kubectl.logs`
	}
}

func generateEnvironmentYAML(name, namespace, domain string) string {
	endpoints := ""
	creds := ""

	switch domain {
	case "kubernetes":
		endpoints = `  endpoints: {}
  # endpoints:
  #   grafana: http://grafana.monitoring:3000
  #   prometheus: http://prometheus.monitoring:9090`
		creds = `  credentials: {}
  # credentials:
  #   grafana-token:
  #     type: bearer-token
  #     secretRef: grafana-viewer-token`
	case "ssh":
		endpoints = `  endpoints: {}
  # Add your target servers here`
		creds = `  credentials:
    ssh-key:
      type: ssh-private-key
      secretRef: ssh-key-secret
      # Or use Vault SSH CA:
      # type: vault-ssh-ca
      # vaultPath: ssh-client-signer
      # vaultRole: agent-role`
	case "http":
		endpoints = `  endpoints:
    # target: https://api.example.com`
		creds = `  credentials: {}
  # credentials:
  #   api-token:
  #     type: bearer-token
  #     secretRef: api-token-secret`
	case "sql":
		endpoints = `  endpoints: {}
  # endpoints:
  #   database: postgresql://db.example:5432/mydb`
		creds = `  credentials:
    database:
      type: vault-database
      vaultPath: database/creds/readonly
      # Or static:
      # type: basic-auth
      # secretRef: db-credentials`
	case "dns":
		endpoints = `  endpoints: {}
  # endpoints:
  #   nameserver: 10.0.0.53`
		creds = `  credentials: {}`
	default:
		endpoints = `  endpoints: {}`
		creds = `  credentials: {}`
	}

	return fmt.Sprintf(`apiVersion: legator.io/v1alpha1
kind: LegatorEnvironment
metadata:
  name: %s-env
  namespace: %s
spec:
  cluster: local

%s

%s
`, name, namespace, endpoints, creds)
}

func generateSkillMD(skill, name, domain string) string {
	switch skill {
	case "cluster-health":
		return `# Cluster Health Monitor

## Role
You are a Kubernetes cluster health monitor. Your job is to detect problems
before they become incidents.

## Checks
1. **Pod Status**: Find pods that are not Running/Succeeded. Focus on CrashLoopBackOff, ImagePullBackOff, Pending.
2. **Node Health**: Check node conditions (Ready, MemoryPressure, DiskPressure, PIDPressure).
3. **Recent Events**: Look for Warning events in the last 15 minutes.
4. **Resource Pressure**: Identify pods with high restart counts (>3).

## Output Format
Report findings in this structure:
- **Critical**: Immediate attention needed (crash loops, node not ready)
- **Warning**: Degraded but functional (high restart counts, pending pods)
- **Healthy**: Summary of what's working

## Budget
Maximum 7 tool calls. Be efficient — combine kubectl queries where possible.
`
	case "pod-restart-monitor":
		return `# Pod Restart Monitor

## Role
You monitor pod restart patterns to catch crash loops early.

## Checks
1. List all pods with restartCount > 0 across all namespaces
2. For pods with restartCount > 3, check logs for the last restart cause
3. Identify OOMKilled vs application errors vs dependency failures

## Output Format
- **Crash Loops**: Pods restarting repeatedly (>3 in last hour)
- **OOMKilled**: Pods killed for memory — include current limits
- **Stable Restarts**: Pods that restarted but stabilised

## Budget
Maximum 5 tool calls.
`
	case "certificate-expiry":
		return `# Certificate Expiry Monitor

## Role
You check TLS certificates across the cluster and report those nearing expiry.

## Checks
1. List all Certificate resources (cert-manager)
2. Check expiry dates — flag anything within 14 days
3. Check for failed certificate issuance (not Ready)

## Output Format
- **Expiring Soon** (<7 days): Immediate action needed
- **Expiring** (7-14 days): Plan renewal
- **Failed**: Certificates that failed to issue
- **Healthy**: Count of valid certificates

## Budget
Maximum 5 tool calls.
`
	case "server-health":
		return `# Server Health Monitor

## Role
You monitor server health via SSH. Check disk, memory, services, and logs.

## Checks
1. Disk usage (df -h) — flag partitions above 85%
2. Memory usage (free -m) — flag if available < 10%
3. Load average (uptime) — flag if > CPU count
4. Failed systemd services (systemctl --failed)
5. Recent error logs (journalctl -p err --since "1 hour ago" --no-pager | tail -20)

## Output Format
- **Critical**: Disk >95%, memory <5%, services down
- **Warning**: Disk >85%, high load, errors in logs
- **Healthy**: Summary

## Budget
Maximum 6 tool calls.
`
	case "custom":
		return fmt.Sprintf(`# %s

## Role
Describe what this agent does.

## Checks
1. First check
2. Second check

## Output Format
Describe the expected output structure.

## Budget
Maximum N tool calls.
`, name)
	default:
		return generateSkillMD("cluster-health", name, domain)
	}
}

func generateActionsYAML(skill, domain string) string {
	switch domain {
	case "kubernetes":
		return `# Action sheet — declares every action this skill may perform.
# Undeclared actions are denied by the runtime.
actions:
  - name: list-pods
    tool: kubectl.get
    tier: read
    description: List pods across namespaces

  - name: describe-resource
    tool: kubectl.describe
    tier: read
    description: Get detailed resource information

  - name: get-logs
    tool: kubectl.logs
    tier: read
    description: Read container logs
`
	case "ssh":
		return `actions:
  - name: check-disk
    tool: ssh.exec
    tier: read
    description: Check disk usage

  - name: check-memory
    tool: ssh.exec
    tier: read
    description: Check memory usage

  - name: check-services
    tool: ssh.exec
    tier: read
    description: Check systemd service status

  - name: check-logs
    tool: ssh.exec
    tier: read
    description: Read recent error logs
`
	case "http":
		return `actions:
  - name: health-check
    tool: http.get
    tier: read
    description: Check endpoint health

  - name: api-query
    tool: http.get
    tier: read
    description: Query API endpoint
`
	case "sql":
		return `actions:
  - name: query-health
    tool: sql.query
    tier: read
    description: Check database health metrics

  - name: query-data
    tool: sql.query
    tier: read
    description: Query application data (read-only)
`
	case "dns":
		return `actions:
  - name: dns-lookup
    tool: dns.query
    tier: read
    description: DNS record lookup

  - name: reverse-lookup
    tool: dns.reverse
    tier: read
    description: Reverse DNS lookup
`
	default:
		return generateActionsYAML(skill, "kubernetes")
	}
}
