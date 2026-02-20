/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package scheduler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
)

// WebhookHandler receives HTTP requests and triggers agent runs.
// It supports generic webhooks and Alertmanager-formatted payloads.
type WebhookHandler struct {
	mu        sync.RWMutex
	log       logr.Logger
	debouncer *Debouncer
	triggers  chan WebhookTrigger

	// agentMap maps source names to agent NamespacedNames.
	// Populated from LegatorAgent trigger specs.
	agentMap map[string][]types.NamespacedName
}

// WebhookTrigger is emitted when a webhook fires for an agent.
type WebhookTrigger struct {
	AgentKey types.NamespacedName
	Source   string
	Payload  string
	Time     time.Time
}

// NewWebhookHandler creates a webhook handler.
func NewWebhookHandler(log logr.Logger, debounceWindow time.Duration) *WebhookHandler {
	return &WebhookHandler{
		log:       log,
		debouncer: NewDebouncer(debounceWindow),
		triggers:  make(chan WebhookTrigger, 100),
		agentMap:  make(map[string][]types.NamespacedName),
	}
}

// Triggers returns the channel of webhook trigger events.
// The scheduler reads from this to initiate agent runs.
func (h *WebhookHandler) Triggers() <-chan WebhookTrigger {
	return h.triggers
}

// RegisterAgent adds a mapping from a source name to an agent.
func (h *WebhookHandler) RegisterAgent(source string, agentKey types.NamespacedName) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agentMap[source] = append(h.agentMap[source], agentKey)
}

// UnregisterAgent removes all mappings for an agent.
func (h *WebhookHandler) UnregisterAgent(agentKey types.NamespacedName) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for source, agents := range h.agentMap {
		var filtered []types.NamespacedName
		for _, a := range agents {
			if a != agentKey {
				filtered = append(filtered, a)
			}
		}
		if len(filtered) == 0 {
			delete(h.agentMap, source)
		} else {
			h.agentMap[source] = filtered
		}
	}
}

// ServeHTTP handles incoming webhook requests.
// Routes:
//   - POST /webhook/{source} — generic webhook
//   - POST /webhook/alertmanager — Alertmanager-formatted
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract source from path: /webhook/{source}
	source := extractSource(r.URL.Path)
	if source == "" {
		http.Error(w, "missing source in path", http.StatusBadRequest)
		return
	}

	// Read payload
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024)) // 1MB max
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	h.log.Info("Webhook received",
		"source", source,
		"contentLength", len(body),
		"contentType", r.Header.Get("Content-Type"),
	)

	// Look up agents for this source
	h.mu.RLock()
	agents, ok := h.agentMap[source]
	h.mu.RUnlock()

	if !ok || len(agents) == 0 {
		h.log.Info("No agents registered for webhook source", "source", source)
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"status":"accepted","agents":0}`)
		return
	}

	// Trigger each matching agent (with debounce)
	triggered := 0
	for _, agentKey := range agents {
		debounceKey := fmt.Sprintf("%s/%s/%s", source, agentKey.Namespace, agentKey.Name)

		if h.debouncer.ShouldFire(debounceKey) {
			select {
			case h.triggers <- WebhookTrigger{
				AgentKey: agentKey,
				Source:   source,
				Payload:  string(body),
				Time:     time.Now(),
			}:
				triggered++
			default:
				h.log.Info("Webhook trigger channel full, dropping",
					"agent", agentKey.String())
			}
		} else {
			h.log.Info("Webhook debounced",
				"source", source,
				"agent", agentKey.String(),
			)
		}
	}

	w.WriteHeader(http.StatusAccepted)
	resp := map[string]interface{}{
		"status":    "accepted",
		"agents":    len(agents),
		"triggered": triggered,
	}
	json.NewEncoder(w).Encode(resp)
}

// extractSource extracts the source name from a webhook URL path.
// e.g. "/webhook/alertmanager" → "alertmanager"
func extractSource(path string) string {
	const prefix = "/webhook/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	source := path[len(prefix):]
	if source == "" {
		return ""
	}
	return source
}

// --- Debouncer (Step 3.8) ---

// Debouncer prevents rapid-fire triggering from bursts of events.
// Within the debounce window, only the first event fires; subsequent ones are dropped.
type Debouncer struct {
	mu     sync.Mutex
	window time.Duration
	last   map[string]time.Time
}

// NewDebouncer creates a debouncer with the given window.
// Default window: 30 seconds.
func NewDebouncer(window time.Duration) *Debouncer {
	if window <= 0 {
		window = 30 * time.Second
	}
	return &Debouncer{
		window: window,
		last:   make(map[string]time.Time),
	}
}

// ShouldFire returns true if the event should proceed.
// Returns false if within the debounce window of the last fire.
func (d *Debouncer) ShouldFire(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	last, exists := d.last[key]
	if exists && now.Sub(last) < d.window {
		return false
	}

	d.last[key] = now
	return true
}

// Reset clears all debounce state.
func (d *Debouncer) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.last = make(map[string]time.Time)
}

// Clean removes entries older than 2× the window to prevent unbounded growth.
func (d *Debouncer) Clean() {
	d.mu.Lock()
	defer d.mu.Unlock()

	threshold := time.Now().Add(-2 * d.window)
	for key, last := range d.last {
		if last.Before(threshold) {
			delete(d.last, key)
		}
	}
}
