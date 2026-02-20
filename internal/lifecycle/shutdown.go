/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package lifecycle provides graceful shutdown coordination for the
// LegatorAgent controller. It tracks in-progress agent runs and ensures
// they either complete or are cleanly terminated before the process exits.
//
// The ShutdownManager integrates with the scheduler's RunTracker to know
// which runs are active, and provides a WaitForDrain method that blocks
// until all runs finish or a hard deadline is reached.
package lifecycle

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// RunTracker is the interface the shutdown manager needs from the scheduler.
type RunTracker interface {
	InFlightCount() int
}

// ShutdownManager coordinates graceful shutdown of in-progress agent runs.
type ShutdownManager struct {
	tracker      RunTracker
	log          logr.Logger
	drainTimeout time.Duration

	// cancels tracks context cancel functions for active runs
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewShutdownManager creates a shutdown coordinator.
// drainTimeout is the maximum time to wait for in-flight runs to complete.
func NewShutdownManager(tracker RunTracker, drainTimeout time.Duration, log logr.Logger) *ShutdownManager {
	return &ShutdownManager{
		tracker:      tracker,
		log:          log.WithName("shutdown"),
		drainTimeout: drainTimeout,
		cancels:      make(map[string]context.CancelFunc),
	}
}

// RegisterRun tracks a run's cancel function so it can be cancelled on hard shutdown.
func (s *ShutdownManager) RegisterRun(key string, cancel context.CancelFunc) {
	s.mu.Lock()
	s.cancels[key] = cancel
	s.mu.Unlock()
}

// DeregisterRun removes a completed run from tracking.
func (s *ShutdownManager) DeregisterRun(key string) {
	s.mu.Lock()
	delete(s.cancels, key)
	s.mu.Unlock()
}

// ActiveRuns returns the number of registered active runs.
func (s *ShutdownManager) ActiveRuns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cancels)
}

// WaitForDrain blocks until all in-flight runs finish or the drain timeout
// is reached. If the timeout expires, it cancels all remaining runs.
//
// Returns the number of runs that were forcibly cancelled.
func (s *ShutdownManager) WaitForDrain() int {
	inflight := s.tracker.InFlightCount()
	if inflight == 0 {
		s.log.Info("No in-flight runs — clean shutdown")
		return 0
	}

	s.log.Info("Waiting for in-flight runs to complete",
		"inflight", inflight,
		"timeout", s.drainTimeout,
	)

	deadline := time.After(s.drainTimeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			remaining := s.tracker.InFlightCount()
			if remaining > 0 {
				s.log.Info("Drain timeout reached — cancelling remaining runs",
					"remaining", remaining,
				)
				s.cancelAll()
				return remaining
			}
			return 0

		case <-ticker.C:
			if s.tracker.InFlightCount() == 0 {
				s.log.Info("All in-flight runs completed — clean shutdown")
				return 0
			}
		}
	}
}

// cancelAll cancels all registered run contexts.
func (s *ShutdownManager) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, cancel := range s.cancels {
		s.log.Info("Cancelling in-flight run", "key", key)
		cancel()
	}
	// Clear the map
	s.cancels = make(map[string]context.CancelFunc)
}
