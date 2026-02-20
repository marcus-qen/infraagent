/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package dashboard

import (
	"testing"
	"time"
)

func TestTimeAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s ago"},
		{90 * time.Second, "1m ago"},
		{3 * time.Hour, "3h ago"},
		{48 * time.Hour, "2d ago"},
	}

	for _, tt := range tests {
		got := timeAgo(time.Now().Add(-tt.d))
		if got != tt.want {
			t.Errorf("timeAgo(-%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTimeAgo_Zero(t *testing.T) {
	got := timeAgo(time.Time{})
	if got != "never" {
		t.Errorf("timeAgo(zero) = %q, want 'never'", got)
	}
}

func TestFormatTime(t *testing.T) {
	got := formatTime(time.Time{})
	if got != "—" {
		t.Errorf("formatTime(zero) = %q, want '—'", got)
	}

	ts := time.Date(2026, 2, 20, 15, 30, 0, 0, time.UTC)
	got = formatTime(ts)
	if got != "2026-02-20 15:30:00" {
		t.Errorf("formatTime = %q, want '2026-02-20 15:30:00'", got)
	}
}

func TestTruncateStr(t *testing.T) {
	if truncateStr("short", 10) != "short" {
		t.Error("short string should not be truncated")
	}
	long := "this is a very long string that should be truncated"
	got := truncateStr(long, 10)
	// "…" is 3 bytes (UTF-8), so max byte length is 10 + 3 = 13
	if len(got) > 13 {
		t.Errorf("truncated too long: %d bytes", len(got))
	}
	if len([]rune(got)) > 11 { // 10 runes + "…"
		t.Errorf("truncated too many runes: %d", len([]rune(got)))
	}
}

func TestStatusIcon(t *testing.T) {
	tests := map[string]string{
		"Succeeded": "✅",
		"Failed":    "❌",
		"Running":   "🔄",
		"Blocked":   "🚫",
		"Ready":     "✅",
		"Pending":   "⏳",
		"Approved":  "✅",
		"Denied":    "❌",
		"Expired":   "⏰",
		"Unknown":   "❓",
	}
	for input, want := range tests {
		got := statusIcon(input)
		if got != want {
			t.Errorf("statusIcon(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSeverityClass(t *testing.T) {
	tests := map[string]string{
		"critical": "severity-critical",
		"warning":  "severity-warning",
		"info":     "severity-info",
		"unknown":  "",
	}
	for input, want := range tests {
		got := severityClass(input)
		if got != want {
			t.Errorf("severityClass(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPct(t *testing.T) {
	if pct(0, 0) != 0 {
		t.Error("pct(0,0) should be 0")
	}
	if pct(7, 10) != 70 {
		t.Errorf("pct(7,10) = %f, want 70", pct(7, 10))
	}
}

func TestDurationMs(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "—"},
		{500, "500ms"},
		{1500, "1.5s"},
		{90000, "1.5m"},
	}
	for _, tt := range tests {
		got := durationMs(tt.ms)
		if got != tt.want {
			t.Errorf("durationMs(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestTokensStr(t *testing.T) {
	tests := []struct {
		in, out int64
		want    string
	}{
		{0, 0, "—"},
		{500, 200, "700"},
		{8000, 3000, "11.0K"},
	}
	for _, tt := range tests {
		got := tokensStr(tt.in, tt.out)
		if got != tt.want {
			t.Errorf("tokensStr(%d,%d) = %q, want %q", tt.in, tt.out, got, tt.want)
		}
	}
}
