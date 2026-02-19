/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	corev1alpha1 "github.com/marcus-qen/infraagent/api/v1alpha1"
)

// --- Slack Channel (Step 4.2) ---

// SlackChannel sends reports to a Slack webhook URL.
type SlackChannel struct {
	name    string
	webhook string
	client  *http.Client
}

func NewSlackChannel(name, webhookURL string) *SlackChannel {
	return &SlackChannel{
		name:    name,
		webhook: webhookURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *SlackChannel) Name() string { return c.name }
func (c *SlackChannel) Type() string { return "slack" }

func (c *SlackChannel) Send(ctx context.Context, report *Report) error {
	payload := formatSlackMessage(report)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.webhook, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send to slack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("slack returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

type slackPayload struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks,omitempty"`
}

type slackBlock struct {
	Type string     `json:"type"`
	Text *slackText `json:"text,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func formatSlackMessage(report *Report) slackPayload {
	icon := severityIcon(report.Severity)
	header := fmt.Sprintf("%s %s %s — %s", icon, report.Emoji, report.Agent, report.Summary)

	blocks := []slackBlock{
		{
			Type: "header",
			Text: &slackText{Type: "plain_text", Text: header},
		},
	}

	if report.Body != "" {
		// Truncate body for Slack (3000 char limit per block)
		body := report.Body
		if len(body) > 2900 {
			body = body[:2900] + "\n… (truncated)"
		}
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{Type: "mrkdwn", Text: body},
		})
	}

	// Findings
	if len(report.Findings) > 0 {
		findingsText := formatFindings(report.Findings)
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{Type: "mrkdwn", Text: findingsText},
		})
	}

	// Usage footer
	if report.Usage != nil {
		usageText := formatUsage(report.Usage)
		blocks = append(blocks, slackBlock{
			Type: "context",
			Text: &slackText{Type: "mrkdwn", Text: usageText},
		})
	}

	return slackPayload{
		Text:   header,
		Blocks: blocks,
	}
}

// --- Telegram Channel (Step 4.3) ---

// TelegramChannel sends reports via the Telegram Bot API.
type TelegramChannel struct {
	name      string
	chatID    string
	secretRef string // name of Secret containing bot token
	botToken  string // resolved at send time or pre-resolved
	client    *http.Client
}

func NewTelegramChannel(name, chatID, secretRef string) *TelegramChannel {
	return &TelegramChannel{
		name:      name,
		chatID:    chatID,
		secretRef: secretRef,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// SetBotToken sets the resolved bot token (called after secret resolution).
func (c *TelegramChannel) SetBotToken(token string) {
	c.botToken = token
}

func (c *TelegramChannel) Name() string { return c.name }
func (c *TelegramChannel) Type() string { return "telegram" }

func (c *TelegramChannel) Send(ctx context.Context, report *Report) error {
	if c.botToken == "" {
		return fmt.Errorf("telegram bot token not set (secretRef: %q)", c.secretRef)
	}

	text := formatTelegramMessage(report)

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)
	payload := map[string]interface{}{
		"chat_id":    c.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send to telegram: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("telegram returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func formatTelegramMessage(report *Report) string {
	var b bytes.Buffer

	icon := severityIcon(report.Severity)
	fmt.Fprintf(&b, "%s *%s %s*\n%s\n", icon, report.Emoji, report.Agent, report.Summary)

	if report.Body != "" {
		body := report.Body
		if len(body) > 3800 { // Telegram 4096 char limit
			body = body[:3800] + "\n… (truncated)"
		}
		fmt.Fprintf(&b, "\n%s\n", body)
	}

	if len(report.Findings) > 0 {
		fmt.Fprintf(&b, "\n%s", formatFindings(report.Findings))
	}

	if report.Usage != nil {
		fmt.Fprintf(&b, "\n_%s_", formatUsage(report.Usage))
	}

	return b.String()
}

// --- Generic Webhook Channel (Step 4.4) ---

// WebhookChannel sends reports as JSON POST to a configurable URL.
type WebhookChannel struct {
	name   string
	url    string
	client *http.Client
}

func NewWebhookChannel(name, webhookURL string) *WebhookChannel {
	return &WebhookChannel{
		name:   name,
		url:    webhookURL,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *WebhookChannel) Name() string { return c.name }
func (c *WebhookChannel) Type() string { return "webhook" }

// WebhookPayload is the JSON structure sent to generic webhook channels.
type WebhookPayload struct {
	Agent      string                      `json:"agent"`
	Emoji      string                      `json:"emoji"`
	RunName    string                      `json:"runName"`
	Severity   string                      `json:"severity"`
	Summary    string                      `json:"summary"`
	Body       string                      `json:"body"`
	Findings   []WebhookFinding            `json:"findings,omitempty"`
	Usage      *WebhookUsage               `json:"usage,omitempty"`
	Guardrails *WebhookGuardrails          `json:"guardrails,omitempty"`
	Timestamp  string                      `json:"timestamp"`
}

type WebhookFinding struct {
	Severity string `json:"severity"`
	Resource string `json:"resource,omitempty"`
	Message  string `json:"message"`
}

type WebhookUsage struct {
	TokensIn    int64  `json:"tokensIn"`
	TokensOut   int64  `json:"tokensOut"`
	TotalTokens int64  `json:"totalTokens"`
	Iterations  int32  `json:"iterations"`
	WallClockMs int64  `json:"wallClockMs"`
	Cost        string `json:"cost,omitempty"`
}

type WebhookGuardrails struct {
	ChecksPerformed      int32  `json:"checksPerformed"`
	ActionsBlocked       int32  `json:"actionsBlocked"`
	EscalationsTriggered int32  `json:"escalationsTriggered"`
	AutonomyCeiling      string `json:"autonomyCeiling"`
}

func (c *WebhookChannel) Send(ctx context.Context, report *Report) error {
	payload := buildWebhookPayload(report)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send to webhook %s: %w", c.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("webhook %s returned %d: %s", c.url, resp.StatusCode, string(respBody))
	}

	return nil
}

func buildWebhookPayload(report *Report) WebhookPayload {
	payload := WebhookPayload{
		Agent:     report.Agent,
		Emoji:     report.Emoji,
		RunName:   report.RunName,
		Severity:  string(report.Severity),
		Summary:   report.Summary,
		Body:      report.Body,
		Timestamp: report.Timestamp.Format(time.RFC3339),
	}

	for _, f := range report.Findings {
		payload.Findings = append(payload.Findings, WebhookFinding{
			Severity: string(f.Severity),
			Resource: f.Resource,
			Message:  f.Message,
		})
	}

	if report.Usage != nil {
		payload.Usage = &WebhookUsage{
			TokensIn:    report.Usage.TokensIn,
			TokensOut:   report.Usage.TokensOut,
			TotalTokens: report.Usage.TotalTokens,
			Iterations:  report.Usage.Iterations,
			WallClockMs: report.Usage.WallClockMs,
			Cost:        report.Usage.EstimatedCost,
		}
	}

	if report.Guardrails != nil {
		payload.Guardrails = &WebhookGuardrails{
			ChecksPerformed:      report.Guardrails.ChecksPerformed,
			ActionsBlocked:       report.Guardrails.ActionsBlocked,
			EscalationsTriggered: report.Guardrails.EscalationsTriggered,
			AutonomyCeiling:      string(report.Guardrails.AutonomyCeiling),
		}
	}

	return payload
}

// --- Formatting helpers (Step 4.5) ---

func severityIcon(s Severity) string {
	switch s {
	case SeveritySuccess:
		return "✅"
	case SeverityInfo:
		return "ℹ️"
	case SeverityWarning:
		return "⚠️"
	case SeverityFailure:
		return "❌"
	case SeverityEscalation:
		return "🚨"
	default:
		return "📋"
	}
}

func formatFindings(findings []corev1alpha1.RunFinding) string {
	var b bytes.Buffer
	b.WriteString("*Findings:*\n")
	for _, f := range findings {
		icon := "ℹ️"
		switch f.Severity {
		case corev1alpha1.FindingSeverityCritical:
			icon = "🔴"
		case corev1alpha1.FindingSeverityWarning:
			icon = "🟡"
		}
		if f.Resource != "" {
			fmt.Fprintf(&b, "%s %s — %s\n", icon, f.Resource, f.Message)
		} else {
			fmt.Fprintf(&b, "%s %s\n", icon, f.Message)
		}
	}
	return b.String()
}

func formatUsage(usage *corev1alpha1.UsageSummary) string {
	parts := []string{
		fmt.Sprintf("tokens: %d", usage.TotalTokens),
		fmt.Sprintf("iterations: %d", usage.Iterations),
	}
	if usage.WallClockMs > 0 {
		dur := time.Duration(usage.WallClockMs) * time.Millisecond
		parts = append(parts, fmt.Sprintf("time: %s", dur.Round(time.Millisecond)))
	}
	if usage.EstimatedCost != "" {
		parts = append(parts, fmt.Sprintf("cost: %s", usage.EstimatedCost))
	}
	return fmt.Sprintf("📊 %s", joinParts(parts))
}

func joinParts(parts []string) string {
	return fmt.Sprintf("%s", concatWith(parts, " | "))
}

func concatWith(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

// --- Mock Channel for testing ---

// MockChannel records all reports sent to it.
type MockChannel struct {
	ChannelName string
	ChannelType string
	Reports     []*Report
	SendError   error
}

func NewMockChannel(name, chType string) *MockChannel {
	return &MockChannel{ChannelName: name, ChannelType: chType}
}

func (m *MockChannel) Name() string { return m.ChannelName }
func (m *MockChannel) Type() string { return m.ChannelType }

func (m *MockChannel) Send(_ context.Context, report *Report) error {
	if m.SendError != nil {
		return m.SendError
	}
	m.Reports = append(m.Reports, report)
	return nil
}

// Ensure URL is imported (used in Telegram)
var _ = url.Values{}
