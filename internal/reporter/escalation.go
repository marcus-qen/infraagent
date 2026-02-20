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
	"fmt"
	"time"

	"github.com/go-logr/logr"

	corev1alpha1 "github.com/marcus-qen/legator/api/v1alpha1"
)

// EscalationEngine handles autonomy-ceiling escalations.
// When an agent attempts an action beyond its autonomy level,
// the engine sends an escalation to the configured channel and
// waits for timeout, then executes the timeout policy.
type EscalationEngine struct {
	reporter *Reporter
	log      logr.Logger
}

// NewEscalationEngine creates an escalation engine.
func NewEscalationEngine(reporter *Reporter, log logr.Logger) *EscalationEngine {
	return &EscalationEngine{
		reporter: reporter,
		log:      log.WithName("escalation"),
	}
}

// EscalationRequest describes what needs to be escalated.
type EscalationRequest struct {
	// Agent is the LegatorAgent that hit the escalation.
	Agent *corev1alpha1.LegatorAgent

	// RunName is the LegatorRun that triggered the escalation.
	RunName string

	// BlockedAction is what the agent tried to do.
	BlockedAction string

	// BlockReason explains why it was blocked.
	BlockReason string

	// ActionTier is the tier of the blocked action.
	ActionTier corev1alpha1.ActionTier
}

// EscalationResult describes what happened with the escalation.
type EscalationResult struct {
	// Sent indicates the escalation was delivered.
	Sent bool

	// TimedOut indicates the escalation timed out waiting for response.
	TimedOut bool

	// Policy is the timeout policy that was applied.
	Policy corev1alpha1.TimeoutAction

	// Error is any error that occurred during escalation.
	Error error
}

// Escalate sends an escalation notification and applies the timeout policy.
func (e *EscalationEngine) Escalate(ctx context.Context, req EscalationRequest) *EscalationResult {
	result := &EscalationResult{}

	escalation := req.Agent.Spec.Guardrails.Escalation
	if escalation == nil {
		e.log.Info("No escalation config, skipping",
			"agent", req.Agent.Name,
			"action", req.BlockedAction,
		)
		return result
	}

	// Build the escalation report
	report := &Report{
		Agent:    req.Agent.Name,
		Emoji:    req.Agent.Spec.Emoji,
		RunName:  req.RunName,
		Severity: SeverityEscalation,
		Summary:  fmt.Sprintf("Action blocked: %s", req.BlockedAction),
		Body: fmt.Sprintf(
			"**Escalation**: Agent `%s` attempted `%s` (tier: %s) but was blocked.\n\n"+
				"**Reason**: %s\n\n"+
				"**Autonomy level**: %s\n\n"+
				"This escalation will %s after %s.",
			req.Agent.Name,
			req.BlockedAction,
			req.ActionTier,
			req.BlockReason,
			req.Agent.Spec.Guardrails.Autonomy,
			describeTimeoutAction(escalation.OnTimeout),
			escalation.Timeout,
		),
		Timestamp: time.Now(),
	}

	// Determine target channel
	channelName := escalation.ChannelName
	if channelName == "" {
		channelName = string(escalation.Target)
	}

	// Send the escalation
	if e.reporter.HasChannel(channelName) {
		if err := e.reporter.Send(ctx, channelName, report); err != nil {
			e.log.Error(err, "Failed to send escalation",
				"channel", channelName,
				"agent", req.Agent.Name,
			)
			result.Error = err
		} else {
			result.Sent = true
		}
	} else {
		e.log.Info("Escalation channel not found, logging only",
			"channel", channelName,
			"agent", req.Agent.Name,
			"action", req.BlockedAction,
			"reason", req.BlockReason,
		)
	}

	// Parse timeout
	timeout, err := time.ParseDuration(escalation.Timeout)
	if err != nil {
		timeout = 5 * time.Minute // default
	}

	// Wait for timeout (in production, this would wait for a response mechanism;
	// for v1, we simply wait and apply the policy)
	e.log.Info("Waiting for escalation timeout",
		"agent", req.Agent.Name,
		"timeout", timeout,
		"onTimeout", escalation.OnTimeout,
	)

	select {
	case <-ctx.Done():
		result.Error = ctx.Err()
		return result
	case <-time.After(timeout):
		result.TimedOut = true
	}

	// Apply timeout policy
	result.Policy = escalation.OnTimeout
	e.log.Info("Escalation timed out, applying policy",
		"agent", req.Agent.Name,
		"policy", escalation.OnTimeout,
	)

	return result
}

func describeTimeoutAction(action corev1alpha1.TimeoutAction) string {
	switch action {
	case corev1alpha1.TimeoutCancel:
		return "cancel the run"
	case corev1alpha1.TimeoutProceed:
		return "proceed with the action"
	case corev1alpha1.TimeoutRetry:
		return "retry the action"
	default:
		return "cancel the run"
	}
}
