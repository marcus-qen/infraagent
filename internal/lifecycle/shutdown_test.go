/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// mockTracker implements RunTracker for tests.
type mockTracker struct {
	count atomic.Int32
}

func (m *mockTracker) InFlightCount() int {
	return int(m.count.Load())
}

func (m *mockTracker) SetCount(n int) {
	m.count.Store(int32(n))
}

func TestWaitForDrain_NoInFlight(t *testing.T) {
	tracker := &mockTracker{}
	log := zap.New(zap.UseDevMode(true))
	sm := NewShutdownManager(tracker, 10*time.Second, log)

	cancelled := sm.WaitForDrain()
	if cancelled != 0 {
		t.Fatalf("expected 0 cancelled, got %d", cancelled)
	}
}

func TestWaitForDrain_RunsCompleteBeforeTimeout(t *testing.T) {
	tracker := &mockTracker{}
	tracker.SetCount(2)
	log := zap.New(zap.UseDevMode(true))
	sm := NewShutdownManager(tracker, 5*time.Second, log)

	// Simulate runs completing after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		tracker.SetCount(0)
	}()

	start := time.Now()
	cancelled := sm.WaitForDrain()
	elapsed := time.Since(start)

	if cancelled != 0 {
		t.Fatalf("expected 0 cancelled, got %d", cancelled)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("took too long: %v", elapsed)
	}
}

func TestWaitForDrain_TimeoutCancelsRemaining(t *testing.T) {
	tracker := &mockTracker{}
	tracker.SetCount(3)
	log := zap.New(zap.UseDevMode(true))
	sm := NewShutdownManager(tracker, 200*time.Millisecond, log)

	// Register fake cancels
	var cancelled1, cancelled2 bool
	_, cancel1 := context.WithCancel(context.Background())
	_, cancel2 := context.WithCancel(context.Background())

	// Wrap to track
	sm.RegisterRun("run-1", func() { cancelled1 = true; cancel1() })
	sm.RegisterRun("run-2", func() { cancelled2 = true; cancel2() })

	// Runs never complete — tracker stays at 3
	result := sm.WaitForDrain()

	if result != 3 {
		t.Fatalf("expected 3 remaining cancelled, got %d", result)
	}
	if !cancelled1 || !cancelled2 {
		t.Fatal("expected all registered runs to be cancelled")
	}
}

func TestRegisterDeregister(t *testing.T) {
	tracker := &mockTracker{}
	log := zap.New(zap.UseDevMode(true))
	sm := NewShutdownManager(tracker, 10*time.Second, log)

	sm.RegisterRun("a", func() {})
	sm.RegisterRun("b", func() {})

	if sm.ActiveRuns() != 2 {
		t.Fatalf("expected 2 active, got %d", sm.ActiveRuns())
	}

	sm.DeregisterRun("a")
	if sm.ActiveRuns() != 1 {
		t.Fatalf("expected 1 active, got %d", sm.ActiveRuns())
	}

	sm.DeregisterRun("b")
	if sm.ActiveRuns() != 0 {
		t.Fatalf("expected 0 active, got %d", sm.ActiveRuns())
	}
}
