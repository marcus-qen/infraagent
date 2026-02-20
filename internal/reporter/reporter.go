/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package reporter delivers agent run reports and escalations to
// notification channels (Slack, Telegram, generic webhook).
//
// The reporter is called by the runner after each run completes.
// It resolves channel names from the LegatorEnvironment, formats
// the message using templates, and delivers via the appropriate transport.
package reporter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	corev1alpha1 "github.com/marcus-qen/legator/api/v1alpha1"
	"github.com/marcus-qen/legator/internal/resolver"
)

// Severity classifies the urgency of a report.
type Severity string

const (
	SeveritySuccess    Severity = "success"
	SeverityInfo       Severity = "info"
	SeverityWarning    Severity = "warning"
	SeverityFailure    Severity = "failure"
	SeverityEscalation Severity = "escalation"
)

// Report is a structured message to be delivered.
type Report struct {
	// Agent is the name of the agent that produced this report.
	Agent string

	// Emoji is the agent's emoji.
	Emoji string

	// RunName is the LegatorRun CR name.
	RunName string

	// Severity classifies the urgency.
	Severity Severity

	// Summary is a short one-line description.
	Summary string

	// Body is the full report text.
	Body string

	// Findings are structured discoveries from the run.
	Findings []corev1alpha1.RunFinding

	// Usage summarises resource consumption.
	Usage *corev1alpha1.UsageSummary

	// Guardrails summarises safety activity.
	Guardrails *corev1alpha1.GuardrailSummary

	// Timestamp is when the report was generated.
	Timestamp time.Time
}

// Channel is the interface for notification transports.
type Channel interface {
	// Send delivers a report to this channel.
	Send(ctx context.Context, report *Report) error

	// Name returns the channel identifier.
	Name() string

	// Type returns the channel type (slack, telegram, webhook).
	Type() string
}

// Reporter resolves channels from the environment and delivers reports.
type Reporter struct {
	log      logr.Logger
	channels map[string]Channel
}

// New creates a Reporter from resolved environment channels.
func New(log logr.Logger, env *resolver.ResolvedEnvironment) *Reporter {
	r := &Reporter{
		log:      log.WithName("reporter"),
		channels: make(map[string]Channel),
	}

	if env == nil || env.Channels == nil {
		return r
	}

	for name, spec := range env.Channels {
		ch, err := newChannelFromSpec(name, spec)
		if err != nil {
			log.Error(err, "Failed to create channel", "channel", name)
			continue
		}
		r.channels[name] = ch
	}

	return r
}

// Send delivers a report to a named channel.
func (r *Reporter) Send(ctx context.Context, channelName string, report *Report) error {
	ch, ok := r.channels[channelName]
	if !ok {
		return fmt.Errorf("channel %q not found (available: %s)",
			channelName, strings.Join(r.ChannelNames(), ", "))
	}

	r.log.Info("Sending report",
		"channel", channelName,
		"agent", report.Agent,
		"severity", report.Severity,
	)

	return ch.Send(ctx, report)
}

// SendToAll delivers a report to all registered channels.
func (r *Reporter) SendToAll(ctx context.Context, report *Report) []error {
	var errs []error
	for name, ch := range r.channels {
		if err := ch.Send(ctx, report); err != nil {
			r.log.Error(err, "Failed to send report", "channel", name)
			errs = append(errs, fmt.Errorf("channel %q: %w", name, err))
		}
	}
	return errs
}

// ChannelNames returns all registered channel names.
func (r *Reporter) ChannelNames() []string {
	names := make([]string, 0, len(r.channels))
	for name := range r.channels {
		names = append(names, name)
	}
	return names
}

// HasChannel returns true if a channel is registered.
func (r *Reporter) HasChannel(name string) bool {
	_, ok := r.channels[name]
	return ok
}

// RegisterChannel adds or replaces a channel.
func (r *Reporter) RegisterChannel(name string, ch Channel) {
	r.channels[name] = ch
}

// newChannelFromSpec creates a Channel implementation from an environment ChannelSpec.
func newChannelFromSpec(name string, spec corev1alpha1.ChannelSpec) (Channel, error) {
	switch spec.Type {
	case "slack":
		return NewSlackChannel(name, spec.Target), nil
	case "telegram":
		return NewTelegramChannel(name, spec.Target, spec.SecretRef), nil
	case "webhook":
		return NewWebhookChannel(name, spec.Target), nil
	default:
		return nil, fmt.Errorf("unsupported channel type: %q", spec.Type)
	}
}

// --- Report building from LegatorRun ---

// FromLegatorRun creates a Report from a completed LegatorRun.
func FromLegatorRun(agent *corev1alpha1.LegatorAgent, run *corev1alpha1.LegatorRun) *Report {
	emoji := agent.Spec.Emoji
	if emoji == "" {
		emoji = "🤖"
	}

	report := &Report{
		Agent:      agent.Name,
		Emoji:      emoji,
		RunName:    run.Name,
		Findings:   run.Status.Findings,
		Usage:      run.Status.Usage,
		Guardrails: run.Status.Guardrails,
		Timestamp:  time.Now(),
	}

	switch run.Status.Phase {
	case corev1alpha1.RunPhaseSucceeded:
		report.Severity = SeveritySuccess
		report.Summary = "Run completed successfully"
	case corev1alpha1.RunPhaseFailed:
		report.Severity = SeverityFailure
		report.Summary = "Run failed"
	case corev1alpha1.RunPhaseEscalated:
		report.Severity = SeverityEscalation
		report.Summary = "Run escalated — action blocked by guardrails"
	case corev1alpha1.RunPhaseBlocked:
		report.Severity = SeverityEscalation
		report.Summary = "Run blocked — all actions denied"
	default:
		report.Severity = SeverityInfo
		report.Summary = fmt.Sprintf("Run ended with phase: %s", run.Status.Phase)
	}

	report.Body = run.Status.Report

	return report
}

// ShouldReport determines whether a report should be sent based on
// the agent's reporting config and the run outcome.
func ShouldReport(reporting *corev1alpha1.ReportingSpec, run *corev1alpha1.LegatorRun) (bool, corev1alpha1.ReportAction) {
	if reporting == nil {
		reporting = &corev1alpha1.ReportingSpec{
			OnSuccess: corev1alpha1.ReportSilent,
			OnFailure: corev1alpha1.ReportEscalate,
			OnFinding: corev1alpha1.ReportLog,
		}
	}

	switch run.Status.Phase {
	case corev1alpha1.RunPhaseSucceeded:
		if len(run.Status.Findings) > 0 {
			return reporting.OnFinding != corev1alpha1.ReportSilent, reporting.OnFinding
		}
		return reporting.OnSuccess != corev1alpha1.ReportSilent, reporting.OnSuccess

	case corev1alpha1.RunPhaseFailed:
		return reporting.OnFailure != corev1alpha1.ReportSilent, reporting.OnFailure

	case corev1alpha1.RunPhaseEscalated, corev1alpha1.RunPhaseBlocked:
		return true, corev1alpha1.ReportEscalate

	default:
		return false, corev1alpha1.ReportSilent
	}
}

// EstimateCost calculates USD cost from token usage and model pricing.
func EstimateCost(usage *corev1alpha1.UsageSummary, inputCostPerMillion, outputCostPerMillion string) string {
	if usage == nil {
		return ""
	}

	inputRate := parseFloat(inputCostPerMillion)
	outputRate := parseFloat(outputCostPerMillion)

	if inputRate == 0 && outputRate == 0 {
		return ""
	}

	cost := (float64(usage.TokensIn) * inputRate / 1_000_000) +
		(float64(usage.TokensOut) * outputRate / 1_000_000)

	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
