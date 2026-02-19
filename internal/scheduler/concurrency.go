/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package scheduler

import (
	"sync"
	"time"
)

// ConcurrencyPolicy defines what to do when a run is requested while another is in progress.
type ConcurrencyPolicy string

const (
	// ConcurrencySkip skips the new run if one is already in progress.
	ConcurrencySkip ConcurrencyPolicy = "Skip"

	// ConcurrencyQueue queues the new run to start after the current one completes.
	// Not yet implemented — defaults to Skip behaviour.
	ConcurrencyQueue ConcurrencyPolicy = "Queue"
)

// RunTracker tracks in-flight agent runs to enforce one-at-a-time concurrency.
// Thread-safe.
type RunTracker struct {
	mu       sync.RWMutex
	inflight map[string]*RunInfo
}

// RunInfo records metadata about an in-flight run.
type RunInfo struct {
	RunName   string
	StartedAt time.Time
}

// NewRunTracker creates a new tracker.
func NewRunTracker() *RunTracker {
	return &RunTracker{
		inflight: make(map[string]*RunInfo),
	}
}

// TryStart attempts to mark an agent as running.
// Returns true if the agent was not already running (run may proceed).
// Returns false if the agent already has an in-flight run (skip this one).
func (t *RunTracker) TryStart(agentKey string, runName string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.inflight[agentKey]; exists {
		return false
	}

	t.inflight[agentKey] = &RunInfo{
		RunName:   runName,
		StartedAt: time.Now(),
	}
	return true
}

// Complete marks an agent run as finished.
func (t *RunTracker) Complete(agentKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.inflight, agentKey)
}

// IsRunning returns true if the agent has an in-flight run.
func (t *RunTracker) IsRunning(agentKey string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, exists := t.inflight[agentKey]
	return exists
}

// GetRunInfo returns info about an in-flight run, or nil if not running.
func (t *RunTracker) GetRunInfo(agentKey string) *RunInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	info, exists := t.inflight[agentKey]
	if !exists {
		return nil
	}
	// Return a copy
	cp := *info
	return &cp
}

// InFlightCount returns how many agents are currently running.
func (t *RunTracker) InFlightCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.inflight)
}

// CleanStale removes runs that have been in-flight longer than the given duration.
// This handles the case where a run crashes without calling Complete().
func (t *RunTracker) CleanStale(maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	cleaned := 0
	now := time.Now()
	for key, info := range t.inflight {
		if now.Sub(info.StartedAt) > maxAge {
			delete(t.inflight, key)
			cleaned++
		}
	}
	return cleaned
}
