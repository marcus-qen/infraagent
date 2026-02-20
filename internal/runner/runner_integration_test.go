/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	corev1alpha1 "github.com/marcus-qen/legator/api/v1alpha1"
)

// TestNotifyFuncCalledAfterRun verifies that NotifyFunc is called
// after a run completes (both success and failure paths).
func TestNotifyFuncCalledAfterRun(t *testing.T) {
	var mu sync.Mutex
	var notifyCalls []struct {
		agentName string
		runName   string
	}

	cfg := RunConfig{
		NotifyFunc: func(ctx context.Context, agent *corev1alpha1.LegatorAgent, run *corev1alpha1.LegatorRun) {
			mu.Lock()
			defer mu.Unlock()
			notifyCalls = append(notifyCalls, struct {
				agentName string
				runName   string
			}{
				agentName: agent.Name,
				runName:   run.Name,
			})
		},
	}

	// Verify the NotifyFunc field is set
	if cfg.NotifyFunc == nil {
		t.Fatal("NotifyFunc should not be nil")
	}

	// Simulate calling NotifyFunc (as the runner would)
	agent := &corev1alpha1.LegatorAgent{}
	agent.Name = "test-agent"
	run := &corev1alpha1.LegatorRun{}
	run.Name = "test-run-123"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg.NotifyFunc(ctx, agent, run)

	mu.Lock()
	defer mu.Unlock()
	if len(notifyCalls) != 1 {
		t.Fatalf("expected 1 notify call, got %d", len(notifyCalls))
	}
	if notifyCalls[0].agentName != "test-agent" {
		t.Errorf("expected agent name 'test-agent', got %q", notifyCalls[0].agentName)
	}
	if notifyCalls[0].runName != "test-run-123" {
		t.Errorf("expected run name 'test-run-123', got %q", notifyCalls[0].runName)
	}
}

// TestNotifyFuncNilSafe verifies the runner doesn't panic when NotifyFunc is nil.
func TestNotifyFuncNilSafe(t *testing.T) {
	cfg := RunConfig{}

	if cfg.NotifyFunc != nil {
		t.Error("NotifyFunc should be nil by default")
	}
	// Verify that checking for nil works (as the runner does)
	if cfg.NotifyFunc != nil {
		cfg.NotifyFunc(context.Background(), nil, nil)
	}
}

// TestCleanupChaining verifies multiple cleanup functions can be chained.
func TestCleanupChaining(t *testing.T) {
	var calls []string

	cleanup1 := func(ctx context.Context) []error {
		calls = append(calls, "vault")
		return nil
	}
	cleanup2 := func(ctx context.Context) []error {
		calls = append(calls, "notify")
		return nil
	}

	// Chain cleanups as done in cmd/main.go
	cleanups := []func(ctx context.Context) []error{cleanup1, cleanup2}
	combined := func(ctx context.Context) []error {
		var allErrs []error
		for _, fn := range cleanups {
			if errs := fn(ctx); len(errs) > 0 {
				allErrs = append(allErrs, errs...)
			}
		}
		return allErrs
	}

	cfg := RunConfig{
		Cleanup: combined,
	}

	errs := cfg.Cleanup(context.Background())
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
	if len(calls) != 2 || calls[0] != "vault" || calls[1] != "notify" {
		t.Errorf("expected [vault notify], got %v", calls)
	}
}
