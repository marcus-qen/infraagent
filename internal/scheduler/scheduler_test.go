/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package scheduler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	corev1alpha1 "github.com/marcus-qen/infraagent/api/v1alpha1"
)

func init() {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))
}

// --- Cron tests (Step 3.1) ---

func TestNextCronRun(t *testing.T) {
	// "every 5 minutes" at 10:02 → next should be 10:05
	now := time.Date(2026, 2, 19, 10, 2, 0, 0, time.UTC)
	next, err := nextCronRun("*/5 * * * *", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Date(2026, 2, 19, 10, 5, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestNextCronRun_Daily(t *testing.T) {
	// "daily at 5am" checked at 6am → next should be tomorrow 5am
	now := time.Date(2026, 2, 19, 6, 0, 0, 0, time.UTC)
	next, err := nextCronRun("0 5 * * *", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Date(2026, 2, 20, 5, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestNextCronRun_InvalidExpr(t *testing.T) {
	_, err := nextCronRun("not-a-cron", time.Now())
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestNextRun_Cron(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Cron:     "*/5 * * * *",
				Timezone: "UTC",
			},
		},
	}

	now := time.Date(2026, 2, 19, 10, 2, 0, 0, time.UTC)
	next, err := NextRun(agent, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Date(2026, 2, 19, 10, 5, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestNextRun_Paused(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{Cron: "*/5 * * * *"},
			Paused:   true,
		},
	}

	next, err := NextRun(agent, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.IsZero() {
		t.Error("paused agent should have zero next run time")
	}
}

func TestNextRun_Timezone(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Cron:     "0 5 * * *",
				Timezone: "America/New_York",
			},
		},
	}

	// 2026-02-19 10:00 UTC = 2026-02-19 05:00 EST (just fired)
	// Next should be 2026-02-20 05:00 EST = 2026-02-20 10:00 UTC
	now := time.Date(2026, 2, 19, 10, 0, 1, 0, time.UTC)
	next, err := NextRun(agent, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Next day at 5am EST
	nyLoc, _ := time.LoadLocation("America/New_York")
	expected := time.Date(2026, 2, 20, 5, 0, 0, 0, nyLoc)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestNextRun_InvalidTimezone(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Cron:     "*/5 * * * *",
				Timezone: "Not/A/Timezone",
			},
		},
	}

	_, err := NextRun(agent, time.Now())
	if err == nil {
		t.Error("expected error for invalid timezone")
	}
}

// --- Interval tests (Step 3.3) ---

func TestNextRun_Interval(t *testing.T) {
	lastRun := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Interval: "5m",
			},
		},
		Status: corev1alpha1.InfraAgentStatus{
			LastRunTime: &metav1.Time{Time: lastRun},
		},
	}

	now := time.Date(2026, 2, 19, 10, 3, 0, 0, time.UTC)
	next, err := NextRun(agent, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := lastRun.Add(5 * time.Minute)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestNextRun_Interval_NeverRun(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Interval: "5m",
			},
		},
	}

	now := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)
	next, err := NextRun(agent, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Never run → due now
	if !next.Equal(now) {
		t.Errorf("expected %v (now), got %v", now, next)
	}
}

func TestNextRun_InvalidInterval(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Interval: "not-a-duration",
			},
		},
	}

	_, err := NextRun(agent, time.Now())
	if err == nil {
		t.Error("expected error for invalid interval")
	}
}

func TestNextRun_TriggerOnly(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Triggers: []corev1alpha1.TriggerSpec{
					{Type: corev1alpha1.TriggerWebhook, Source: "alertmanager"},
				},
			},
		},
	}

	next, err := NextRun(agent, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.IsZero() {
		t.Error("trigger-only agent should have zero next run time")
	}
}

// --- IsDue tests ---

func TestIsDue_NeverRun(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{Cron: "*/5 * * * *"},
		},
	}

	due, err := IsDue(agent, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !due {
		t.Error("agent that has never run should be due")
	}
}

func TestIsDue_RecentRun(t *testing.T) {
	now := time.Date(2026, 2, 19, 10, 3, 0, 0, time.UTC)
	lastRun := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC) // ran at :00

	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Cron:     "*/5 * * * *",
				Timezone: "UTC",
			},
		},
		Status: corev1alpha1.InfraAgentStatus{
			LastRunTime: &metav1.Time{Time: lastRun},
		},
	}

	// Next cron after lastRun is :05, now is :03 → not due
	due, err := IsDue(agent, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if due {
		t.Error("agent should not be due before next cron tick")
	}
}

func TestIsDue_PastDue(t *testing.T) {
	now := time.Date(2026, 2, 19, 10, 6, 0, 0, time.UTC)
	lastRun := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)

	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{
				Cron:     "*/5 * * * *",
				Timezone: "UTC",
			},
		},
		Status: corev1alpha1.InfraAgentStatus{
			LastRunTime: &metav1.Time{Time: lastRun},
		},
	}

	// Next cron after lastRun(:00) is :05, now is :06 → due
	due, err := IsDue(agent, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !due {
		t.Error("agent should be due after cron tick")
	}
}

func TestIsDue_Paused(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{Cron: "*/5 * * * *"},
			Paused:   true,
		},
	}

	due, err := IsDue(agent, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if due {
		t.Error("paused agent should never be due")
	}
}

// --- Jitter tests (Step 3.4) ---

func TestApplyJitter_Bounded(t *testing.T) {
	base := time.Date(2026, 2, 19, 10, 5, 0, 0, time.UTC)
	interval := 5 * time.Minute

	// Run 100 times, verify all within bounds
	for i := 0; i < 100; i++ {
		result := ApplyJitter(base, interval, 10.0)
		diff := result.Sub(base).Abs()

		// 10% of 5 min = 30s, so jitter should be within ±15s
		// But we cap at 30s total, so ±15s
		if diff > 30*time.Second {
			t.Errorf("jitter %v exceeds 30s cap", diff)
		}
	}
}

func TestApplyJitter_ZeroPercent(t *testing.T) {
	base := time.Date(2026, 2, 19, 10, 5, 0, 0, time.UTC)
	// 0% → uses default 10%
	result := ApplyJitter(base, 5*time.Minute, 0)
	diff := result.Sub(base).Abs()
	if diff > 30*time.Second {
		t.Errorf("jitter %v exceeds 30s cap", diff)
	}
}

func TestApplyJitter_SmallInterval(t *testing.T) {
	base := time.Date(2026, 2, 19, 10, 5, 0, 0, time.UTC)
	// Very small interval → jitter below minimum → no jitter applied
	result := ApplyJitter(base, 500*time.Millisecond, 10.0)
	if !result.Equal(base) {
		t.Error("very small interval should not have jitter")
	}
}

func TestComputeInterval_FromInterval(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{Interval: "5m"},
		},
	}

	dur := ComputeInterval(agent)
	if dur != 5*time.Minute {
		t.Errorf("expected 5m, got %v", dur)
	}
}

func TestComputeInterval_FromCron(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{
		Spec: corev1alpha1.InfraAgentSpec{
			Schedule: corev1alpha1.ScheduleSpec{Cron: "*/5 * * * *"},
		},
	}

	dur := ComputeInterval(agent)
	if dur != 5*time.Minute {
		t.Errorf("expected 5m, got %v", dur)
	}
}

// --- Concurrency tests (Step 3.5) ---

func TestRunTracker_TryStart(t *testing.T) {
	tracker := NewRunTracker()

	// First start succeeds
	if !tracker.TryStart("ns/agent1", "run1") {
		t.Error("first TryStart should succeed")
	}

	// Second start fails (already running)
	if tracker.TryStart("ns/agent1", "run2") {
		t.Error("second TryStart should fail (already running)")
	}

	// Different agent succeeds
	if !tracker.TryStart("ns/agent2", "run3") {
		t.Error("different agent TryStart should succeed")
	}

	if tracker.InFlightCount() != 2 {
		t.Errorf("expected 2 in-flight, got %d", tracker.InFlightCount())
	}
}

func TestRunTracker_Complete(t *testing.T) {
	tracker := NewRunTracker()

	tracker.TryStart("ns/agent1", "run1")
	if !tracker.IsRunning("ns/agent1") {
		t.Error("should be running after TryStart")
	}

	tracker.Complete("ns/agent1")
	if tracker.IsRunning("ns/agent1") {
		t.Error("should not be running after Complete")
	}

	// Can start again
	if !tracker.TryStart("ns/agent1", "run2") {
		t.Error("should be able to start again after Complete")
	}
}

func TestRunTracker_GetRunInfo(t *testing.T) {
	tracker := NewRunTracker()

	if tracker.GetRunInfo("ns/agent1") != nil {
		t.Error("should return nil for non-running agent")
	}

	tracker.TryStart("ns/agent1", "my-run")
	info := tracker.GetRunInfo("ns/agent1")
	if info == nil {
		t.Fatal("should return info for running agent")
	}
	if info.RunName != "my-run" {
		t.Errorf("expected run name 'my-run', got %q", info.RunName)
	}
}

func TestRunTracker_CleanStale(t *testing.T) {
	tracker := NewRunTracker()

	tracker.TryStart("ns/agent1", "run1")
	// Hack: manually set start time to 1 hour ago
	tracker.inflight["ns/agent1"].StartedAt = time.Now().Add(-1 * time.Hour)

	cleaned := tracker.CleanStale(30 * time.Minute)
	if cleaned != 1 {
		t.Errorf("expected 1 cleaned, got %d", cleaned)
	}
	if tracker.IsRunning("ns/agent1") {
		t.Error("stale run should have been cleaned")
	}
}

// --- Debouncer tests (Step 3.8) ---

func TestDebouncer_FirstAlwaysFires(t *testing.T) {
	d := NewDebouncer(30 * time.Second)
	if !d.ShouldFire("key1") {
		t.Error("first call should always fire")
	}
}

func TestDebouncer_SecondWithinWindowDropped(t *testing.T) {
	d := NewDebouncer(30 * time.Second)
	d.ShouldFire("key1")

	if d.ShouldFire("key1") {
		t.Error("second call within window should be dropped")
	}
}

func TestDebouncer_DifferentKeysIndependent(t *testing.T) {
	d := NewDebouncer(30 * time.Second)
	d.ShouldFire("key1")

	if !d.ShouldFire("key2") {
		t.Error("different key should fire independently")
	}
}

func TestDebouncer_DefaultWindow(t *testing.T) {
	d := NewDebouncer(0) // Should default to 30s
	if d.window != 30*time.Second {
		t.Errorf("expected 30s default, got %v", d.window)
	}
}

func TestDebouncer_Reset(t *testing.T) {
	d := NewDebouncer(30 * time.Second)
	d.ShouldFire("key1")
	d.Reset()

	// After reset, should fire again
	if !d.ShouldFire("key1") {
		t.Error("should fire after reset")
	}
}

// --- Webhook handler tests (Step 3.7) ---

func TestWebhookHandler_BasicTrigger(t *testing.T) {
	log := logf.Log.WithName("test")
	h := NewWebhookHandler(log, 1*time.Millisecond)

	agentKey := types.NamespacedName{Namespace: "default", Name: "watchman-light"}
	h.RegisterAgent("alertmanager", agentKey)

	// Send webhook
	body := `{"alerts": [{"status": "firing"}]}`
	req := httptest.NewRequest("POST", "/webhook/alertmanager", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}

	// Check trigger was emitted
	select {
	case trigger := <-h.Triggers():
		if trigger.AgentKey != agentKey {
			t.Errorf("expected agent key %v, got %v", agentKey, trigger.AgentKey)
		}
		if trigger.Source != "alertmanager" {
			t.Errorf("expected source 'alertmanager', got %q", trigger.Source)
		}
	case <-time.After(1 * time.Second):
		t.Error("timed out waiting for trigger")
	}
}

func TestWebhookHandler_UnknownSource(t *testing.T) {
	log := logf.Log.WithName("test")
	h := NewWebhookHandler(log, 30*time.Second)

	req := httptest.NewRequest("POST", "/webhook/unknown", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202 even for unknown source, got %d", rec.Code)
	}

	// No trigger should be emitted
	select {
	case <-h.Triggers():
		t.Error("should not emit trigger for unknown source")
	case <-time.After(50 * time.Millisecond):
		// Expected — no trigger
	}
}

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	log := logf.Log.WithName("test")
	h := NewWebhookHandler(log, 30*time.Second)

	req := httptest.NewRequest("GET", "/webhook/alertmanager", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestWebhookHandler_MissingSource(t *testing.T) {
	log := logf.Log.WithName("test")
	h := NewWebhookHandler(log, 30*time.Second)

	req := httptest.NewRequest("POST", "/webhook/", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestWebhookHandler_Debounce(t *testing.T) {
	log := logf.Log.WithName("test")
	h := NewWebhookHandler(log, 5*time.Second) // 5s window

	agentKey := types.NamespacedName{Namespace: "default", Name: "agent1"}
	h.RegisterAgent("test-source", agentKey)

	// First request fires
	req1 := httptest.NewRequest("POST", "/webhook/test-source", strings.NewReader("{}"))
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)

	// Drain trigger
	select {
	case <-h.Triggers():
	case <-time.After(1 * time.Second):
		t.Fatal("first trigger should fire")
	}

	// Second request within window — debounced
	req2 := httptest.NewRequest("POST", "/webhook/test-source", strings.NewReader("{}"))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	select {
	case <-h.Triggers():
		t.Error("second trigger should be debounced")
	case <-time.After(50 * time.Millisecond):
		// Expected — debounced
	}
}

func TestWebhookHandler_UnregisterAgent(t *testing.T) {
	log := logf.Log.WithName("test")
	h := NewWebhookHandler(log, 1*time.Millisecond)

	agentKey := types.NamespacedName{Namespace: "default", Name: "agent1"}
	h.RegisterAgent("source1", agentKey)
	h.UnregisterAgent(agentKey)

	req := httptest.NewRequest("POST", "/webhook/source1", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	select {
	case <-h.Triggers():
		t.Error("should not trigger after unregister")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

// --- extractSource tests ---

func TestExtractSource(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/webhook/alertmanager", "alertmanager"},
		{"/webhook/custom-source", "custom-source"},
		{"/webhook/", ""},
		{"/webhook", ""},
		{"/other/path", ""},
	}

	for _, tt := range tests {
		got := extractSource(tt.path)
		if got != tt.want {
			t.Errorf("extractSource(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
