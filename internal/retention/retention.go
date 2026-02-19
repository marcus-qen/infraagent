/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package retention provides automatic cleanup of old AgentRun CRs.
// It runs as a manager.Runnable inside the controller-runtime manager,
// periodically scanning for AgentRuns past their TTL and deleting them.
//
// Configuration:
//   - TTL: How long to keep completed AgentRuns (default 7 days)
//   - ScanInterval: How often to check (default 1 hour)
//   - MaxDeleteBatch: Max AgentRuns to delete per scan (default 100)
//   - PreserveMinPerAgent: Keep at least N runs per agent regardless of TTL
package retention

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/marcus-qen/infraagent/api/v1alpha1"
)

// Config configures the retention controller.
type Config struct {
	// TTL is how long completed AgentRuns are retained.
	TTL time.Duration

	// ScanInterval is how often the cleaner runs.
	ScanInterval time.Duration

	// MaxDeleteBatch is the maximum number of AgentRuns to delete per scan.
	MaxDeleteBatch int

	// PreserveMinPerAgent keeps at least this many runs per agent, even if older than TTL.
	PreserveMinPerAgent int
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig() Config {
	return Config{
		TTL:                 7 * 24 * time.Hour,
		ScanInterval:        1 * time.Hour,
		MaxDeleteBatch:      100,
		PreserveMinPerAgent: 5,
	}
}

// Controller cleans up old AgentRun CRs.
type Controller struct {
	client client.Client
	config Config
	log    logr.Logger
	now    func() time.Time // injectable clock for testing
}

// NewController creates a retention controller.
func NewController(c client.Client, cfg Config, log logr.Logger) *Controller {
	return &Controller{
		client: c,
		config: cfg,
		log:    log.WithName("retention"),
		now:    time.Now,
	}
}

// Start implements manager.Runnable.
func (c *Controller) Start(ctx context.Context) error {
	c.log.Info("Retention controller starting",
		"ttl", c.config.TTL,
		"scanInterval", c.config.ScanInterval,
		"maxDeleteBatch", c.config.MaxDeleteBatch,
		"preserveMinPerAgent", c.config.PreserveMinPerAgent,
	)

	// Run immediately on start, then on interval
	c.scan(ctx)

	ticker := time.NewTicker(c.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Retention controller stopping")
			return nil
		case <-ticker.C:
			c.scan(ctx)
		}
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
// Only the leader should clean up AgentRuns.
func (c *Controller) NeedLeaderElection() bool {
	return true
}

// ScanResult captures what happened in a single scan.
type ScanResult struct {
	Scanned  int
	Eligible int
	Deleted  int
	Errors   int
}

// scan lists all AgentRuns and deletes those past TTL.
func (c *Controller) scan(ctx context.Context) {
	result := c.doScan(ctx)

	if result.Deleted > 0 || result.Errors > 0 {
		c.log.Info("Retention scan complete",
			"scanned", result.Scanned,
			"eligible", result.Eligible,
			"deleted", result.Deleted,
			"errors", result.Errors,
		)
	} else {
		c.log.V(1).Info("Retention scan complete — nothing to clean",
			"scanned", result.Scanned,
		)
	}
}

// doScan performs the actual scan logic (extracted for testability).
func (c *Controller) doScan(ctx context.Context) ScanResult {
	var result ScanResult

	// List all AgentRuns across all namespaces
	runList := &corev1alpha1.AgentRunList{}
	if err := c.client.List(ctx, runList); err != nil {
		c.log.Error(err, "Failed to list AgentRuns for retention scan")
		result.Errors++
		return result
	}

	result.Scanned = len(runList.Items)
	now := c.now()
	cutoff := now.Add(-c.config.TTL)

	// Group runs by agent for PreserveMinPerAgent
	byAgent := make(map[string][]*corev1alpha1.AgentRun)
	for i := range runList.Items {
		run := &runList.Items[i]
		agentKey := fmt.Sprintf("%s/%s", run.Namespace, run.Spec.AgentRef)
		byAgent[agentKey] = append(byAgent[agentKey], run)
	}

	// Sort each agent's runs by creation time (newest first)
	for _, runs := range byAgent {
		sort.Slice(runs, func(i, j int) bool {
			return runs[i].CreationTimestamp.After(runs[j].CreationTimestamp.Time)
		})
	}

	// Identify eligible runs
	var toDelete []*corev1alpha1.AgentRun
	for _, runs := range byAgent {
		for i, run := range runs {
			// Skip non-terminal runs
			if !isTerminal(run.Status.Phase) {
				continue
			}

			// Preserve minimum per agent
			if i < c.config.PreserveMinPerAgent {
				continue
			}

			// Check TTL
			completionTime := run.Status.CompletionTime
			if completionTime == nil {
				// Fall back to creation time
				completionTime = &run.CreationTimestamp
			}
			if completionTime.Time.Before(cutoff) {
				toDelete = append(toDelete, run)
			}
		}
	}

	result.Eligible = len(toDelete)

	// Apply batch limit
	if len(toDelete) > c.config.MaxDeleteBatch {
		toDelete = toDelete[:c.config.MaxDeleteBatch]
	}

	// Delete eligible runs
	for _, run := range toDelete {
		if err := c.client.Delete(ctx, run); err != nil {
			c.log.Error(err, "Failed to delete expired AgentRun",
				"agentRun", run.Name,
				"namespace", run.Namespace,
				"agent", run.Spec.AgentRef,
			)
			result.Errors++
		} else {
			result.Deleted++
		}
	}

	return result
}

// isTerminal returns true if the phase represents a completed run.
func isTerminal(phase corev1alpha1.RunPhase) bool {
	switch phase {
	case corev1alpha1.RunPhaseSucceeded,
		corev1alpha1.RunPhaseFailed,
		corev1alpha1.RunPhaseEscalated,
		corev1alpha1.RunPhaseBlocked:
		return true
	default:
		return false
	}
}
