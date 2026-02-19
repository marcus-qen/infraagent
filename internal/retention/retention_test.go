/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package retention

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	corev1alpha1 "github.com/marcus-qen/infraagent/api/v1alpha1"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1alpha1.AddToScheme(s)
	return s
}

func makeRun(name, ns, agent string, phase corev1alpha1.RunPhase, createdAt time.Time, completedAt *time.Time) *corev1alpha1.AgentRun {
	run := &corev1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			CreationTimestamp: metav1.Time{Time: createdAt},
		},
		Spec: corev1alpha1.AgentRunSpec{
			AgentRef:       agent,
			EnvironmentRef: "env",
			Trigger:        corev1alpha1.RunTriggerScheduled,
		},
		Status: corev1alpha1.AgentRunStatus{
			Phase: phase,
		},
	}
	if completedAt != nil {
		run.Status.CompletionTime = &metav1.Time{Time: *completedAt}
	}
	return run
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestRetention_NoRuns(t *testing.T) {
	s := newScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	log := zap.New(zap.UseDevMode(true))

	ctrl := NewController(fc, DefaultConfig(), log)
	result := ctrl.doScan(context.Background())

	if result.Scanned != 0 || result.Deleted != 0 {
		t.Fatalf("expected 0/0, got scanned=%d deleted=%d", result.Scanned, result.Deleted)
	}
}

func TestRetention_NothingExpired(t *testing.T) {
	now := time.Now()
	s := newScheme()
	runs := []corev1alpha1.AgentRun{
		*makeRun("run-1", "default", "watchman", corev1alpha1.RunPhaseSucceeded, now.Add(-1*time.Hour), timePtr(now.Add(-1*time.Hour))),
		*makeRun("run-2", "default", "watchman", corev1alpha1.RunPhaseSucceeded, now.Add(-2*time.Hour), timePtr(now.Add(-2*time.Hour))),
	}

	fc := fake.NewClientBuilder().WithScheme(s).
		WithLists(&corev1alpha1.AgentRunList{Items: runs}).
		Build()
	log := zap.New(zap.UseDevMode(true))

	cfg := DefaultConfig()
	cfg.TTL = 24 * time.Hour
	ctrl := NewController(fc, cfg, log)
	ctrl.now = func() time.Time { return now }

	result := ctrl.doScan(context.Background())
	if result.Scanned != 2 {
		t.Fatalf("expected 2 scanned, got %d", result.Scanned)
	}
	if result.Deleted != 0 {
		t.Fatalf("expected 0 deleted, got %d", result.Deleted)
	}
}

func TestRetention_DeletesExpired(t *testing.T) {
	now := time.Now()
	s := newScheme()

	runs := []corev1alpha1.AgentRun{
		// Recent — should be kept
		*makeRun("run-recent", "default", "watchman", corev1alpha1.RunPhaseSucceeded,
			now.Add(-1*time.Hour), timePtr(now.Add(-1*time.Hour))),
		// Old — past TTL, but within preserveMin
		*makeRun("run-old-1", "default", "watchman", corev1alpha1.RunPhaseSucceeded,
			now.Add(-8*24*time.Hour), timePtr(now.Add(-8*24*time.Hour))),
		// Very old — past TTL and past preserveMin
		*makeRun("run-old-2", "default", "watchman", corev1alpha1.RunPhaseFailed,
			now.Add(-10*24*time.Hour), timePtr(now.Add(-10*24*time.Hour))),
		*makeRun("run-old-3", "default", "watchman", corev1alpha1.RunPhaseSucceeded,
			now.Add(-12*24*time.Hour), timePtr(now.Add(-12*24*time.Hour))),
		*makeRun("run-old-4", "default", "watchman", corev1alpha1.RunPhaseSucceeded,
			now.Add(-14*24*time.Hour), timePtr(now.Add(-14*24*time.Hour))),
		*makeRun("run-old-5", "default", "watchman", corev1alpha1.RunPhaseEscalated,
			now.Add(-16*24*time.Hour), timePtr(now.Add(-16*24*time.Hour))),
		*makeRun("run-old-6", "default", "watchman", corev1alpha1.RunPhaseBlocked,
			now.Add(-20*24*time.Hour), timePtr(now.Add(-20*24*time.Hour))),
	}

	fc := fake.NewClientBuilder().WithScheme(s).
		WithLists(&corev1alpha1.AgentRunList{Items: runs}).
		Build()
	log := zap.New(zap.UseDevMode(true))

	cfg := Config{
		TTL:                 7 * 24 * time.Hour,
		ScanInterval:        1 * time.Hour,
		MaxDeleteBatch:      100,
		PreserveMinPerAgent: 3, // keep newest 3 regardless
	}
	ctrl := NewController(fc, cfg, log)
	ctrl.now = func() time.Time { return now }

	result := ctrl.doScan(context.Background())

	// 7 total runs sorted newest first:
	//   run-recent (1h old) — preserved (index 0)
	//   run-old-1 (8d old) — preserved (index 1)
	//   run-old-2 (10d old) — preserved (index 2, within preserveMin=3)
	//   run-old-3 (12d old) — eligible (index 3, past TTL 7d)
	//   run-old-4 (14d old) — eligible
	//   run-old-5 (16d old) — eligible
	//   run-old-6 (20d old) — eligible
	// Expected: 4 eligible, 4 deleted
	if result.Eligible != 4 {
		t.Fatalf("expected 4 eligible, got %d", result.Eligible)
	}
	if result.Deleted != 4 {
		t.Fatalf("expected 4 deleted, got %d", result.Deleted)
	}

	// Verify remaining
	remaining := &corev1alpha1.AgentRunList{}
	_ = fc.List(context.Background(), remaining)
	if len(remaining.Items) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(remaining.Items))
	}
}

func TestRetention_SkipsNonTerminal(t *testing.T) {
	now := time.Now()
	s := newScheme()

	runs := []corev1alpha1.AgentRun{
		*makeRun("run-pending", "default", "forge", corev1alpha1.RunPhasePending,
			now.Add(-30*24*time.Hour), nil),
		*makeRun("run-running", "default", "forge", corev1alpha1.RunPhaseRunning,
			now.Add(-30*24*time.Hour), nil),
	}

	fc := fake.NewClientBuilder().WithScheme(s).
		WithLists(&corev1alpha1.AgentRunList{Items: runs}).
		Build()
	log := zap.New(zap.UseDevMode(true))

	cfg := DefaultConfig()
	cfg.PreserveMinPerAgent = 0
	ctrl := NewController(fc, cfg, log)
	ctrl.now = func() time.Time { return now }

	result := ctrl.doScan(context.Background())
	if result.Deleted != 0 {
		t.Fatalf("expected 0 deleted for non-terminal runs, got %d", result.Deleted)
	}
}

func TestRetention_BatchLimit(t *testing.T) {
	now := time.Now()
	s := newScheme()

	var runs []corev1alpha1.AgentRun
	for i := 0; i < 20; i++ {
		runs = append(runs, *makeRun(
			fmt.Sprintf("run-%d", i), "default", "bulk-agent",
			corev1alpha1.RunPhaseSucceeded,
			now.Add(-time.Duration(30+i)*24*time.Hour),
			timePtr(now.Add(-time.Duration(30+i)*24*time.Hour)),
		))
	}

	fc := fake.NewClientBuilder().WithScheme(s).
		WithLists(&corev1alpha1.AgentRunList{Items: runs}).
		Build()
	log := zap.New(zap.UseDevMode(true))

	cfg := Config{
		TTL:                 7 * 24 * time.Hour,
		ScanInterval:        1 * time.Hour,
		MaxDeleteBatch:      5, // only delete 5 per scan
		PreserveMinPerAgent: 0,
	}
	ctrl := NewController(fc, cfg, log)
	ctrl.now = func() time.Time { return now }

	result := ctrl.doScan(context.Background())
	if result.Eligible != 20 {
		t.Fatalf("expected 20 eligible, got %d", result.Eligible)
	}
	if result.Deleted != 5 {
		t.Fatalf("expected 5 deleted (batch limit), got %d", result.Deleted)
	}
}

func TestRetention_MultipleAgents(t *testing.T) {
	now := time.Now()
	s := newScheme()

	runs := []corev1alpha1.AgentRun{
		// Agent A: 3 runs, 1 expired
		*makeRun("a-1", "ns", "agent-a", corev1alpha1.RunPhaseSucceeded,
			now.Add(-1*time.Hour), timePtr(now.Add(-1*time.Hour))),
		*makeRun("a-2", "ns", "agent-a", corev1alpha1.RunPhaseSucceeded,
			now.Add(-10*24*time.Hour), timePtr(now.Add(-10*24*time.Hour))),
		*makeRun("a-3", "ns", "agent-a", corev1alpha1.RunPhaseSucceeded,
			now.Add(-20*24*time.Hour), timePtr(now.Add(-20*24*time.Hour))),
		// Agent B: 2 runs, 1 expired
		*makeRun("b-1", "ns", "agent-b", corev1alpha1.RunPhaseSucceeded,
			now.Add(-2*time.Hour), timePtr(now.Add(-2*time.Hour))),
		*makeRun("b-2", "ns", "agent-b", corev1alpha1.RunPhaseSucceeded,
			now.Add(-15*24*time.Hour), timePtr(now.Add(-15*24*time.Hour))),
	}

	fc := fake.NewClientBuilder().WithScheme(s).
		WithLists(&corev1alpha1.AgentRunList{Items: runs}).
		Build()
	log := zap.New(zap.UseDevMode(true))

	cfg := Config{
		TTL:                 7 * 24 * time.Hour,
		ScanInterval:        1 * time.Hour,
		MaxDeleteBatch:      100,
		PreserveMinPerAgent: 1, // keep newest 1
	}
	ctrl := NewController(fc, cfg, log)
	ctrl.now = func() time.Time { return now }

	result := ctrl.doScan(context.Background())

	// Agent A: sorted newest first: a-1, a-2, a-3
	//   a-1 (index 0): preserved
	//   a-2 (index 1, 10d old, past TTL): eligible
	//   a-3 (index 2, 20d old, past TTL): eligible
	// Agent B: sorted: b-1, b-2
	//   b-1 (index 0): preserved
	//   b-2 (index 1, 15d old, past TTL): eligible
	// Total: 3 eligible
	if result.Eligible != 3 {
		t.Fatalf("expected 3 eligible, got %d", result.Eligible)
	}
	if result.Deleted != 3 {
		t.Fatalf("expected 3 deleted, got %d", result.Deleted)
	}
}

func TestRetention_NeedLeaderElection(t *testing.T) {
	s := newScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	log := zap.New(zap.UseDevMode(true))
	ctrl := NewController(fc, DefaultConfig(), log)

	if !ctrl.NeedLeaderElection() {
		t.Fatal("retention controller must require leader election")
	}
}

// Ensure fmt is used (it's imported for Sprintf in makeRun names in TestRetention_BatchLimit)
var _ = fmt.Sprintf
