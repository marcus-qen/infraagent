/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reporter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1alpha1 "github.com/marcus-qen/infraagent/api/v1alpha1"
)

// --- Report building tests ---

func TestFromAgentRun_Succeeded(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{}
	agent.Name = "watchman-light"
	agent.Spec.Emoji = "👁️"

	run := &corev1alpha1.AgentRun{}
	run.Name = "watchman-light-abc"
	run.Status.Phase = corev1alpha1.RunPhaseSucceeded
	run.Status.Report = "All endpoints healthy"
	run.Status.Usage = &corev1alpha1.UsageSummary{
		TotalTokens: 1500,
		Iterations:  3,
	}

	report := FromAgentRun(agent, run)

	if report.Severity != SeveritySuccess {
		t.Errorf("expected success severity, got %s", report.Severity)
	}
	if report.Agent != "watchman-light" {
		t.Errorf("expected agent name 'watchman-light', got %q", report.Agent)
	}
	if report.Emoji != "👁️" {
		t.Errorf("expected emoji '👁️', got %q", report.Emoji)
	}
	if report.Body != "All endpoints healthy" {
		t.Errorf("expected body 'All endpoints healthy', got %q", report.Body)
	}
}

func TestFromAgentRun_Failed(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{}
	agent.Name = "forge"

	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseFailed

	report := FromAgentRun(agent, run)

	if report.Severity != SeverityFailure {
		t.Errorf("expected failure severity, got %s", report.Severity)
	}
	if report.Emoji != "🤖" {
		t.Errorf("expected default emoji, got %q", report.Emoji)
	}
}

func TestFromAgentRun_Escalated(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{}
	agent.Name = "vigil"

	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseEscalated

	report := FromAgentRun(agent, run)

	if report.Severity != SeverityEscalation {
		t.Errorf("expected escalation severity, got %s", report.Severity)
	}
}

func TestFromAgentRun_Blocked(t *testing.T) {
	agent := &corev1alpha1.InfraAgent{}
	agent.Name = "scout"

	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseBlocked

	report := FromAgentRun(agent, run)

	if report.Severity != SeverityEscalation {
		t.Errorf("expected escalation severity for blocked, got %s", report.Severity)
	}
}

// --- ShouldReport tests ---

func TestShouldReport_SuccessSilent(t *testing.T) {
	reporting := &corev1alpha1.ReportingSpec{
		OnSuccess: corev1alpha1.ReportSilent,
		OnFailure: corev1alpha1.ReportEscalate,
		OnFinding: corev1alpha1.ReportLog,
	}

	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseSucceeded

	should, _ := ShouldReport(reporting, run)
	if should {
		t.Error("should not report on silent success")
	}
}

func TestShouldReport_SuccessNotify(t *testing.T) {
	reporting := &corev1alpha1.ReportingSpec{
		OnSuccess: corev1alpha1.ReportNotify,
	}

	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseSucceeded

	should, action := ShouldReport(reporting, run)
	if !should {
		t.Error("should report when onSuccess=notify")
	}
	if action != corev1alpha1.ReportNotify {
		t.Errorf("expected notify action, got %s", action)
	}
}

func TestShouldReport_FailureEscalate(t *testing.T) {
	reporting := &corev1alpha1.ReportingSpec{
		OnFailure: corev1alpha1.ReportEscalate,
	}

	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseFailed

	should, action := ShouldReport(reporting, run)
	if !should {
		t.Error("should report on failure")
	}
	if action != corev1alpha1.ReportEscalate {
		t.Errorf("expected escalate action, got %s", action)
	}
}

func TestShouldReport_FindingsOverrideSuccess(t *testing.T) {
	reporting := &corev1alpha1.ReportingSpec{
		OnSuccess: corev1alpha1.ReportSilent,
		OnFinding: corev1alpha1.ReportNotify,
	}

	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseSucceeded
	run.Status.Findings = []corev1alpha1.RunFinding{
		{Severity: corev1alpha1.FindingSeverityWarning, Message: "Something found"},
	}

	should, action := ShouldReport(reporting, run)
	if !should {
		t.Error("should report when success has findings and onFinding=notify")
	}
	if action != corev1alpha1.ReportNotify {
		t.Errorf("expected notify action, got %s", action)
	}
}

func TestShouldReport_EscalatedAlwaysReports(t *testing.T) {
	reporting := &corev1alpha1.ReportingSpec{
		OnSuccess: corev1alpha1.ReportSilent,
		OnFailure: corev1alpha1.ReportSilent,
	}

	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseEscalated

	should, action := ShouldReport(reporting, run)
	if !should {
		t.Error("escalated runs should always report")
	}
	if action != corev1alpha1.ReportEscalate {
		t.Errorf("expected escalate action, got %s", action)
	}
}

func TestShouldReport_NilReporting(t *testing.T) {
	run := &corev1alpha1.AgentRun{}
	run.Status.Phase = corev1alpha1.RunPhaseFailed

	should, _ := ShouldReport(nil, run)
	if !should {
		t.Error("default reporting should report failures")
	}
}

// --- Mock channel tests ---

func TestMockChannel(t *testing.T) {
	mock := NewMockChannel("test", "mock")

	report := &Report{
		Agent:    "test-agent",
		Severity: SeveritySuccess,
		Summary:  "All good",
	}

	if err := mock.Send(context.Background(), report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.Reports) != 1 {
		t.Errorf("expected 1 report, got %d", len(mock.Reports))
	}
	if mock.Reports[0].Agent != "test-agent" {
		t.Errorf("wrong agent in report")
	}
}

func TestMockChannel_Error(t *testing.T) {
	mock := NewMockChannel("test", "mock")
	mock.SendError = fmt.Errorf("channel down")

	err := mock.Send(context.Background(), &Report{})
	if err == nil {
		t.Error("expected error")
	}
}

// --- Reporter tests ---

func TestReporter_SendToMock(t *testing.T) {
	reporter := &Reporter{
		channels: make(map[string]Channel),
	}

	mock := NewMockChannel("oncall", "mock")
	reporter.RegisterChannel("oncall", mock)

	report := &Report{
		Agent:    "watchman",
		Severity: SeverityWarning,
		Summary:  "Pod restarting",
	}

	err := reporter.Send(context.Background(), "oncall", report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Reports) != 1 {
		t.Errorf("expected 1 report, got %d", len(mock.Reports))
	}
}

func TestReporter_SendToUnknownChannel(t *testing.T) {
	reporter := &Reporter{
		channels: make(map[string]Channel),
	}

	err := reporter.Send(context.Background(), "nonexistent", &Report{})
	if err == nil {
		t.Error("expected error for unknown channel")
	}
}

func TestReporter_SendToAll(t *testing.T) {
	reporter := &Reporter{
		channels: make(map[string]Channel),
	}

	mock1 := NewMockChannel("slack", "mock")
	mock2 := NewMockChannel("telegram", "mock")
	reporter.RegisterChannel("slack", mock1)
	reporter.RegisterChannel("telegram", mock2)

	report := &Report{Agent: "test", Severity: SeverityInfo}
	errs := reporter.SendToAll(context.Background(), report)
	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(mock1.Reports) != 1 {
		t.Error("slack should have 1 report")
	}
	if len(mock2.Reports) != 1 {
		t.Error("telegram should have 1 report")
	}
}

func TestReporter_HasChannel(t *testing.T) {
	reporter := &Reporter{
		channels: make(map[string]Channel),
	}

	reporter.RegisterChannel("oncall", NewMockChannel("oncall", "mock"))

	if !reporter.HasChannel("oncall") {
		t.Error("should have 'oncall' channel")
	}
	if reporter.HasChannel("missing") {
		t.Error("should not have 'missing' channel")
	}
}

// --- Webhook channel integration test ---

func TestWebhookChannel_Integration(t *testing.T) {
	var received WebhookPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewWebhookChannel("test-webhook", server.URL)
	report := &Report{
		Agent:     "watchman-light",
		Emoji:     "👁️",
		RunName:   "watchman-light-xyz",
		Severity:  SeverityWarning,
		Summary:   "Found 2 issues",
		Body:      "Details here",
		Timestamp: time.Date(2026, 2, 19, 21, 0, 0, 0, time.UTC),
		Findings: []corev1alpha1.RunFinding{
			{Severity: corev1alpha1.FindingSeverityWarning, Message: "Pod restarting"},
		},
		Usage: &corev1alpha1.UsageSummary{
			TokensIn:    1000,
			TokensOut:   500,
			TotalTokens: 1500,
			Iterations:  3,
			WallClockMs: 5000,
		},
	}

	err := ch.Send(context.Background(), report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.Agent != "watchman-light" {
		t.Errorf("expected agent 'watchman-light', got %q", received.Agent)
	}
	if received.Severity != "warning" {
		t.Errorf("expected severity 'warning', got %q", received.Severity)
	}
	if len(received.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(received.Findings))
	}
	if received.Usage == nil {
		t.Error("expected usage in payload")
	} else if received.Usage.TotalTokens != 1500 {
		t.Errorf("expected 1500 total tokens, got %d", received.Usage.TotalTokens)
	}
}

func TestWebhookChannel_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	ch := NewWebhookChannel("test", server.URL)
	err := ch.Send(context.Background(), &Report{})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

// --- Cost estimation tests ---

func TestEstimateCost(t *testing.T) {
	usage := &corev1alpha1.UsageSummary{
		TokensIn:  10000,
		TokensOut: 5000,
	}

	// Anthropic Claude Sonnet pricing: $3/1M input, $15/1M output
	cost := EstimateCost(usage, "3.0", "15.0")

	// Expected: (10000 * 3.0 / 1M) + (5000 * 15.0 / 1M) = 0.03 + 0.075 = 0.105
	if cost != "$0.10" && cost != "$0.11" {
		t.Errorf("expected '$0.10' or '$0.11', got %q", cost)
	}
}

func TestEstimateCost_SmallAmount(t *testing.T) {
	usage := &corev1alpha1.UsageSummary{
		TokensIn:  100,
		TokensOut: 50,
	}

	cost := EstimateCost(usage, "3.0", "15.0")
	// Expected: very small — should use 4 decimal places
	if cost != "$0.0010" && cost != "$0.0011" {
		t.Errorf("expected '$0.0010' or '$0.0011', got %q", cost)
	}
}

func TestEstimateCost_NoPricing(t *testing.T) {
	usage := &corev1alpha1.UsageSummary{TokensIn: 100}

	cost := EstimateCost(usage, "", "")
	if cost != "" {
		t.Errorf("expected empty cost when no pricing, got %q", cost)
	}
}

func TestEstimateCost_NilUsage(t *testing.T) {
	cost := EstimateCost(nil, "3.0", "15.0")
	if cost != "" {
		t.Errorf("expected empty cost for nil usage, got %q", cost)
	}
}

// --- Formatting tests ---

func TestSeverityIcon(t *testing.T) {
	tests := []struct {
		severity Severity
		icon     string
	}{
		{SeveritySuccess, "✅"},
		{SeverityFailure, "❌"},
		{SeverityEscalation, "🚨"},
		{SeverityWarning, "⚠️"},
		{SeverityInfo, "ℹ️"},
	}

	for _, tt := range tests {
		got := severityIcon(tt.severity)
		if got != tt.icon {
			t.Errorf("severityIcon(%s) = %q, want %q", tt.severity, got, tt.icon)
		}
	}
}

func TestFormatUsage(t *testing.T) {
	usage := &corev1alpha1.UsageSummary{
		TotalTokens: 1500,
		Iterations:  3,
		WallClockMs: 5200,
	}

	text := formatUsage(usage)
	if text == "" {
		t.Error("expected non-empty usage text")
	}
	if !contains(text, "1500") {
		t.Error("usage should contain token count")
	}
	if !contains(text, "3") {
		t.Error("usage should contain iteration count")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
