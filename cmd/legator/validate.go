/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// handleValidate checks an agent directory for common problems.
func handleValidate(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: legator validate <directory>")
		os.Exit(1)
	}

	dir := args[0]
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", dir)
		os.Exit(1)
	}

	fmt.Printf("🔍 Validating agent in %s ...\n\n", dir)

	errors := 0
	warnings := 0

	// Check agent.yaml exists
	agentPath := filepath.Join(dir, "agent.yaml")
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		printError("agent.yaml not found")
		errors++
	} else {
		e, w := validateAgentYAML(agentPath)
		errors += e
		warnings += w
	}

	// Check environment.yaml exists
	envPath := filepath.Join(dir, "environment.yaml")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		printWarning("environment.yaml not found (will need one before deploying)")
		warnings++
	} else {
		e, w := validateEnvironmentYAML(envPath)
		errors += e
		warnings += w
	}

	// Check skill directory
	skillDir := filepath.Join(dir, "skill")
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		printError("skill/ directory not found")
		errors++
	} else {
		e, w := validateSkill(skillDir)
		errors += e
		warnings += w
	}

	// Summary
	fmt.Println()
	if errors > 0 {
		fmt.Printf("❌ Validation failed: %d error(s), %d warning(s)\n", errors, warnings)
		os.Exit(1)
	} else if warnings > 0 {
		fmt.Printf("⚠️  Validation passed with %d warning(s)\n", warnings)
	} else {
		fmt.Println("✅ Validation passed — agent is ready to deploy")
	}
}

func validateAgentYAML(path string) (errors, warnings int) {
	data, err := os.ReadFile(path)
	if err != nil {
		printError(fmt.Sprintf("cannot read agent.yaml: %v", err))
		return 1, 0
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		printError(fmt.Sprintf("agent.yaml is not valid YAML: %v", err))
		return 1, 0
	}

	printOK("agent.yaml is valid YAML")

	// Check apiVersion
	api, _ := doc["apiVersion"].(string)
	if api != "legator.io/v1alpha1" {
		printError(fmt.Sprintf("apiVersion should be legator.io/v1alpha1, got %q", api))
		errors++
	}

	// Check kind
	kind, _ := doc["kind"].(string)
	if kind != "LegatorAgent" {
		printError(fmt.Sprintf("kind should be LegatorAgent, got %q", kind))
		errors++
	}

	// Check metadata
	meta, _ := doc["metadata"].(map[string]interface{})
	if meta == nil {
		printError("metadata is missing")
		errors++
	} else {
		if name, _ := meta["name"].(string); name == "" {
			printError("metadata.name is required")
			errors++
		} else {
			printOK(fmt.Sprintf("agent name: %s", name))
		}
		if ns, _ := meta["namespace"].(string); ns == "" {
			printWarning("metadata.namespace not set (will use 'default')")
			warnings++
		}
	}

	// Check spec
	spec, _ := doc["spec"].(map[string]interface{})
	if spec == nil {
		printError("spec is missing")
		return errors + 1, warnings
	}

	// Autonomy
	autonomy, _ := spec["autonomy"].(string)
	validAutonomy := map[string]bool{
		"observe": true, "recommend": true,
		"automate-safe": true, "automate-destructive": true,
	}
	if autonomy == "" {
		printError("spec.autonomy is required")
		errors++
	} else if !validAutonomy[autonomy] {
		printError(fmt.Sprintf("spec.autonomy %q is not valid (observe|recommend|automate-safe|automate-destructive)", autonomy))
		errors++
	} else {
		printOK(fmt.Sprintf("autonomy: %s", autonomy))
		if autonomy == "automate-destructive" {
			printWarning("automate-destructive autonomy — ensure guardrails are properly configured")
			warnings++
		}
	}

	// Model tier
	tier, _ := spec["modelTier"].(string)
	validTiers := map[string]bool{"fast": true, "standard": true, "reasoning": true}
	if tier != "" && !validTiers[tier] {
		printWarning(fmt.Sprintf("modelTier %q is non-standard (expected fast|standard|reasoning)", tier))
		warnings++
	}

	// Schedule
	schedule, _ := spec["schedule"].(map[string]interface{})
	if schedule == nil {
		printWarning("no schedule defined — agent will only run on annotation trigger or webhook")
		warnings++
	} else {
		cron, _ := schedule["cron"].(string)
		if cron != "" {
			parts := strings.Fields(cron)
			if len(parts) != 5 {
				printError(fmt.Sprintf("cron schedule %q should have 5 fields", cron))
				errors++
			} else {
				printOK(fmt.Sprintf("schedule: %s", cron))
			}
		}
	}

	// Guardrails
	guardrails, _ := spec["guardrails"].(map[string]interface{})
	if guardrails == nil {
		printWarning("no guardrails defined — defaults will apply")
		warnings++
	} else {
		if budget, ok := guardrails["tokenBudget"]; ok {
			budgetNum := toInt(budget)
			if budgetNum > 100000 {
				printWarning(fmt.Sprintf("tokenBudget %d is very high — consider reducing", budgetNum))
				warnings++
			}
			if budgetNum < 1000 {
				printWarning(fmt.Sprintf("tokenBudget %d may be too low for useful work", budgetNum))
				warnings++
			}
		}
		if maxIter, ok := guardrails["maxIterations"]; ok {
			iterNum := toInt(maxIter)
			if iterNum > 30 {
				printWarning(fmt.Sprintf("maxIterations %d is very high — risk of runaway", iterNum))
				warnings++
			}
		}
		tools, _ := guardrails["allowedTools"].([]interface{})
		if len(tools) == 0 {
			printWarning("no allowedTools specified — agent will have no tools")
			warnings++
		} else {
			printOK(fmt.Sprintf("%d tool(s) allowed", len(tools)))
		}
	}

	// Skill reference
	skill, _ := spec["skill"].(map[string]interface{})
	if skill == nil {
		printError("spec.skill is required")
		errors++
	} else {
		source, _ := skill["source"].(string)
		if source == "" {
			printError("spec.skill.source is required")
			errors++
		} else {
			printOK(fmt.Sprintf("skill source: %s", source))
		}
	}

	// Environment reference
	envRef, _ := spec["environmentRef"].(map[string]interface{})
	if envRef == nil {
		printWarning("no environmentRef — agent won't have endpoints or credentials")
		warnings++
	}

	return errors, warnings
}

func validateEnvironmentYAML(path string) (errors, warnings int) {
	data, err := os.ReadFile(path)
	if err != nil {
		printError(fmt.Sprintf("cannot read environment.yaml: %v", err))
		return 1, 0
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		printError(fmt.Sprintf("environment.yaml is not valid YAML: %v", err))
		return 1, 0
	}

	printOK("environment.yaml is valid YAML")

	api, _ := doc["apiVersion"].(string)
	if api != "legator.io/v1alpha1" {
		printError(fmt.Sprintf("apiVersion should be legator.io/v1alpha1, got %q", api))
		errors++
	}

	kind, _ := doc["kind"].(string)
	if kind != "LegatorEnvironment" {
		printError(fmt.Sprintf("kind should be LegatorEnvironment, got %q", kind))
		errors++
	}

	return errors, warnings
}

func validateSkill(dir string) (errors, warnings int) {
	// Check SKILL.md
	skillPath := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		printError("skill/SKILL.md not found")
		errors++
	} else {
		data, _ := os.ReadFile(skillPath)
		content := string(data)
		if len(content) < 50 {
			printWarning("skill/SKILL.md is very short — add more detail for the LLM")
			warnings++
		} else {
			printOK(fmt.Sprintf("skill/SKILL.md (%d bytes)", len(content)))
		}

		// Check for budget directive
		if !strings.Contains(strings.ToLower(content), "budget") {
			printWarning("skill/SKILL.md has no budget directive — consider adding one to control tool call count")
			warnings++
		}
	}

	// Check actions.yaml
	actionsPath := filepath.Join(dir, "actions.yaml")
	if _, err := os.Stat(actionsPath); os.IsNotExist(err) {
		printWarning("skill/actions.yaml not found — undeclared actions may be denied at runtime")
		warnings++
	} else {
		data, _ := os.ReadFile(actionsPath)
		var doc map[string]interface{}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			printError(fmt.Sprintf("skill/actions.yaml is not valid YAML: %v", err))
			errors++
		} else {
			actions, _ := doc["actions"].([]interface{})
			if len(actions) == 0 {
				printWarning("skill/actions.yaml has no actions defined")
				warnings++
			} else {
				printOK(fmt.Sprintf("skill/actions.yaml: %d action(s) defined", len(actions)))
			}
		}
	}

	return errors, warnings
}

func printOK(msg string) {
	fmt.Printf("  ✅ %s\n", msg)
}

func printError(msg string) {
	fmt.Printf("  ❌ %s\n", msg)
}

func printWarning(msg string) {
	fmt.Printf("  ⚠️  %s\n", msg)
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}
