/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package scheduler implements agent scheduling — cron, interval, webhook triggers,
// jitter, concurrency control, and pause/resume.
package scheduler

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/robfig/cron/v3"

	corev1alpha1 "github.com/marcus-qen/infraagent/api/v1alpha1"
)

// NextRun computes the next scheduled run time for an agent.
// Returns zero time if the agent has no schedule or is paused.
func NextRun(agent *corev1alpha1.InfraAgent, now time.Time) (time.Time, error) {
	if agent.Spec.Paused {
		return time.Time{}, nil
	}

	spec := agent.Spec.Schedule

	// Load timezone
	loc, err := loadTimezone(spec.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", spec.Timezone, err)
	}
	nowInTZ := now.In(loc)

	// Cron takes priority over interval
	if spec.Cron != "" {
		return nextCronRun(spec.Cron, nowInTZ)
	}

	if spec.Interval != "" {
		return nextIntervalRun(spec.Interval, agent, now)
	}

	// Trigger-only agents (webhook/event) have no schedule
	return time.Time{}, nil
}

// IsDue returns true if the agent should run now.
// An agent is due if its next scheduled run time has passed
// and it hasn't already run since that time.
func IsDue(agent *corev1alpha1.InfraAgent, now time.Time) (bool, error) {
	if agent.Spec.Paused {
		return false, nil
	}

	// Must have a cron or interval schedule
	if agent.Spec.Schedule.Cron == "" && agent.Spec.Schedule.Interval == "" {
		return false, nil
	}

	// If never run, it's due
	if agent.Status.LastRunTime == nil {
		return true, nil
	}

	lastRun := agent.Status.LastRunTime.Time

	// Compute what the next run after the last run would be
	nextAfterLast, err := nextRunAfter(agent, lastRun)
	if err != nil {
		return false, err
	}

	return !nextAfterLast.IsZero() && now.After(nextAfterLast), nil
}

// --- Cron (Step 3.1) ---

// nextCronRun parses a cron expression and returns the next fire time after now.
func nextCronRun(expr string, now time.Time) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return sched.Next(now), nil
}

// nextRunAfter computes the next run time after a given reference time.
func nextRunAfter(agent *corev1alpha1.InfraAgent, after time.Time) (time.Time, error) {
	spec := agent.Spec.Schedule

	loc, err := loadTimezone(spec.Timezone)
	if err != nil {
		return time.Time{}, err
	}

	if spec.Cron != "" {
		return nextCronRun(spec.Cron, after.In(loc))
	}

	if spec.Interval != "" {
		dur, err := time.ParseDuration(spec.Interval)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid interval %q: %w", spec.Interval, err)
		}
		return after.Add(dur), nil
	}

	return time.Time{}, nil
}

// --- Interval (Step 3.3) ---

// nextIntervalRun computes the next interval-based run.
// If the agent has never run, it's due immediately.
func nextIntervalRun(interval string, agent *corev1alpha1.InfraAgent, now time.Time) (time.Time, error) {
	dur, err := time.ParseDuration(interval)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid interval %q: %w", interval, err)
	}

	if agent.Status.LastRunTime == nil {
		// Never run — due now
		return now, nil
	}

	return agent.Status.LastRunTime.Time.Add(dur), nil
}

// --- Timezone ---

func loadTimezone(tz string) (*time.Location, error) {
	if tz == "" || tz == "UTC" {
		return time.UTC, nil
	}
	return time.LoadLocation(tz)
}

// --- Jitter (Step 3.4) ---

// ApplyJitter adds random jitter to a scheduled time.
// Jitter is ±(jitterPercent/2)% of the interval, so agents with
// identical cron expressions don't all fire at the same instant.
//
// Default jitter: 10% of interval (capped at 30s).
func ApplyJitter(scheduled time.Time, interval time.Duration, jitterPercent float64) time.Time {
	if jitterPercent <= 0 {
		jitterPercent = 10.0
	}

	maxJitter := time.Duration(float64(interval) * jitterPercent / 100.0)

	// Cap jitter at 30s to prevent excessive delays
	if maxJitter > 30*time.Second {
		maxJitter = 30 * time.Second
	}

	// Minimum jitter of 100ms to avoid precision issues
	if maxJitter < 100*time.Millisecond {
		return scheduled
	}

	// Random offset in [-maxJitter/2, +maxJitter/2]
	offset := time.Duration(rand.Int63n(int64(maxJitter))) - maxJitter/2

	return scheduled.Add(offset)
}

// ComputeInterval returns the effective scheduling interval for jitter calculation.
func ComputeInterval(agent *corev1alpha1.InfraAgent) time.Duration {
	spec := agent.Spec.Schedule

	if spec.Interval != "" {
		dur, err := time.ParseDuration(spec.Interval)
		if err == nil {
			return dur
		}
	}

	// For cron, estimate interval from the expression
	if spec.Cron != "" {
		now := time.Now()
		next1, err := nextCronRun(spec.Cron, now)
		if err == nil {
			next2, err := nextCronRun(spec.Cron, next1)
			if err == nil {
				return next2.Sub(next1)
			}
		}
	}

	// Default: 5 minutes
	return 5 * time.Minute
}
