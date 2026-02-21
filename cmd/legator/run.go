package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// handleRunAgent handles "legator run <agent> [--target X] [--task "..."] [--wait]"
func handleRunAgent(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: legator run <agent> [--target <device>] [--task \"...\"] [--wait]")
		os.Exit(1)
	}

	agentName := args[0]
	target := ""
	task := ""
	wait := false

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--target", "-t":
			if i+1 < len(args) {
				target = args[i+1]
				i++
			}
		case "--task":
			if i+1 < len(args) {
				task = args[i+1]
				i++
			}
		case "--wait", "-w":
			wait = true
		}
	}

	dc, defaultNS, err := getClient()
	fatal(err)

	ns := getNamespace(args)
	if ns == "" {
		ns = defaultNS
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get the agent
	agent, err := dc.Resource(agentGVR).Namespace(ns).Get(ctx, agentName, metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Agent %q not found: %v\n", agentName, err)
		os.Exit(1)
	}

	// Set annotations to trigger a run
	annotations := agent.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["legator.io/run-now"] = "true"
	if task != "" {
		annotations["legator.io/task"] = task
	}
	if target != "" {
		annotations["legator.io/target"] = target
	}
	agent.SetAnnotations(annotations)

	_, err = dc.Resource(agentGVR).Namespace(ns).Update(ctx, agent, metav1.UpdateOptions{})
	fatal(err)

	emoji := getNestedString(*agent, "spec", "emoji")
	autonomy := getNestedString(*agent, "spec", "guardrails", "autonomy")

	fmt.Printf("%s Triggered %s (autonomy: %s)\n", emoji, agentName, autonomy)
	if task != "" {
		fmt.Printf("   Task: %s\n", task)
	}
	if target != "" {
		fmt.Printf("   Target: %s\n", target)
	}

	if !wait {
		fmt.Println("\nRun started. Use 'legator runs list --agent", agentName+"' to check progress.")
		return
	}

	// Wait for run to complete
	fmt.Println("\nWaiting for run to complete...")

	// Poll for new run (created after our trigger)
	triggerTime := time.Now()
	var runName string

	for i := 0; i < 60; i++ {
		time.Sleep(5 * time.Second)

		list, err := dc.Resource(runGVR).Namespace(ns).List(ctx, metav1.ListOptions{
			LabelSelector: "legator.io/agent=" + agentName,
		})
		if err != nil {
			continue
		}

		for _, item := range list.Items {
			ct := item.GetCreationTimestamp()
			if ct.After(triggerTime) {
				runName = item.GetName()
				phase := getNestedString(item, "status", "phase")

				switch phase {
				case "Succeeded":
					report := getNestedString(item, "status", "report")
					fmt.Printf("\n✅ %s completed successfully\n", runName)
					if report != "" {
						fmt.Printf("\n--- Report ---\n%s\n", report)
					}
					return
				case "Failed":
					fmt.Printf("\n❌ %s failed\n", runName)
					report := getNestedString(item, "status", "report")
					if report != "" {
						fmt.Printf("\n--- Report ---\n%s\n", report)
					}
					os.Exit(1)
				default:
					fmt.Printf("  [%s] %s...\r", formatDuration(time.Since(triggerTime)), phase)
				}
			}
		}
	}

	if runName != "" {
		fmt.Printf("\n⏰ Timed out waiting. Last seen: %s\n", runName)
	} else {
		fmt.Println("\n⏰ Timed out. Run may not have started yet.")
	}
	_ = strings.TrimSpace // suppress unused import
	os.Exit(1)
}

// handleCheck handles "legator check <target>" — quick health probe
func handleCheck(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: legator check <target>")
		os.Exit(1)
	}

	target := args[0]

	// Trigger watchman-light with target
	handleRunAgent([]string{"watchman-light", "--target", target, "--task",
		fmt.Sprintf("Quick health check on %s", target), "--wait"})
}

// handleInventory handles "legator inventory [list|show <name>]"
func handleInventory(args []string) {
	dc, defaultNS, err := getClient()
	fatal(err)

	ns := getNamespace(args)
	if ns == "" {
		ns = defaultNS
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// List environments and extract endpoints
	envs, err := dc.Resource(envGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	fatal(err)

	if len(args) > 0 && (args[0] == "show" || args[0] == "get") {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: legator inventory show <name>")
			os.Exit(1)
		}
		inventoryShow(envs, args[1])
		return
	}

	// List all endpoints across environments
	fmt.Println("📋 Infrastructure Inventory")
	fmt.Println("───────────────────────────")

	total := 0
	for _, env := range envs.Items {
		envName := env.GetName()
		endpoints, found, _ := unstructured.NestedMap(env.Object, "spec", "endpoints")
		if !found {
			continue
		}

		for name, ep := range endpoints {
			epMap, ok := ep.(map[string]interface{})
			if !ok {
				continue
			}
			url, _ := epMap["url"].(string)
			fmt.Printf("  %-25s %s  (env: %s)\n", name, url, envName)
			total++
		}
	}

	fmt.Printf("\n%d endpoints across %d environments\n", total, len(envs.Items))
}

func inventoryShow(envs *unstructured.UnstructuredList, target string) {
	for _, env := range envs.Items {
		endpoints, found, _ := unstructured.NestedMap(env.Object, "spec", "endpoints")
		if !found {
			continue
		}

		if ep, ok := endpoints[target]; ok {
			epMap, _ := ep.(map[string]interface{})
			url, _ := epMap["url"].(string)

			fmt.Printf("📍 %s\n", target)
			fmt.Printf("  URL:         %s\n", url)
			fmt.Printf("  Environment: %s\n", env.GetName())

			// Check if there's a credential reference
			creds, found, _ := unstructured.NestedMap(env.Object, "spec", "credentials")
			if found {
				for name := range creds {
					fmt.Printf("  Credential:  %s\n", name)
				}
			}
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Endpoint %q not found in any environment\n", target)
	os.Exit(1)
}
