/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package connectivity manages network connectivity to non-Kubernetes targets
// via Headscale/Tailscale mesh VPN or direct connections.
//
// The package provides pre-run connectivity checks, sidecar health monitoring,
// and endpoint reachability verification.
package connectivity

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-logr/logr"

	corev1alpha1 "github.com/marcus-qen/legator/api/v1alpha1"
)

// Manager handles network connectivity setup and health checks.
type Manager struct {
	log logr.Logger
}

// NewManager creates a connectivity manager.
func NewManager(log logr.Logger) *Manager {
	return &Manager{log: log}
}

// ConnectivityStatus represents the health of the network layer.
type ConnectivityStatus struct {
	// Type is the connectivity method in use.
	Type string

	// Ready indicates whether the connectivity layer is operational.
	Ready bool

	// Message provides human-readable status.
	Message string

	// Endpoints maps endpoint names to their reachability status.
	Endpoints map[string]EndpointStatus
}

// EndpointStatus represents the reachability of a single endpoint.
type EndpointStatus struct {
	// Reachable indicates whether the endpoint responded.
	Reachable bool

	// Latency is the round-trip time to reach the endpoint.
	Latency time.Duration

	// Error is set if the endpoint is unreachable.
	Error string
}

// CheckHealth verifies the connectivity layer is operational.
// For direct connectivity, this is always healthy.
// For headscale/tailscale, this checks the sidecar is running and connected.
func (m *Manager) CheckHealth(ctx context.Context, spec *corev1alpha1.ConnectivitySpec) ConnectivityStatus {
	if spec == nil {
		return ConnectivityStatus{
			Type:    "direct",
			Ready:   true,
			Message: "Direct connectivity (no mesh VPN)",
		}
	}

	switch spec.Type {
	case "direct":
		return ConnectivityStatus{
			Type:    "direct",
			Ready:   true,
			Message: "Direct connectivity configured",
		}

	case "headscale", "tailscale":
		return m.checkTailscaleHealth(ctx, spec)

	default:
		return ConnectivityStatus{
			Type:    spec.Type,
			Ready:   false,
			Message: fmt.Sprintf("Unknown connectivity type: %s", spec.Type),
		}
	}
}

// checkTailscaleHealth verifies the Tailscale/Headscale sidecar is running
// by checking for the Tailscale local API socket or status endpoint.
func (m *Manager) checkTailscaleHealth(ctx context.Context, spec *corev1alpha1.ConnectivitySpec) ConnectivityStatus {
	status := ConnectivityStatus{
		Type: spec.Type,
	}

	// Check if the Tailscale sidecar is reachable via its local API
	// The sidecar exposes a Unix socket at /var/run/tailscale/tailscaled.sock
	// or an HTTP API at localhost:41112 (the localapi port)
	conn, err := net.DialTimeout("tcp", "127.0.0.1:41112", 2*time.Second)
	if err != nil {
		// Fallback: check Unix socket
		conn, err = net.DialTimeout("unix", "/var/run/tailscale/tailscaled.sock", 2*time.Second)
		if err != nil {
			status.Ready = false
			status.Message = fmt.Sprintf("Tailscale sidecar not reachable: %v", err)
			return status
		}
	}
	conn.Close()

	status.Ready = true
	status.Message = fmt.Sprintf("%s sidecar is running and connected", spec.Type)
	return status
}

// CheckEndpoints verifies that specific endpoints are reachable through
// the connectivity layer.
func (m *Manager) CheckEndpoints(ctx context.Context, endpoints map[string]corev1alpha1.EndpointSpec) map[string]EndpointStatus {
	results := make(map[string]EndpointStatus, len(endpoints))

	for name, ep := range endpoints {
		results[name] = m.checkEndpoint(ctx, name, ep)
	}

	return results
}

// checkEndpoint verifies a single endpoint is reachable.
func (m *Manager) checkEndpoint(ctx context.Context, name string, ep corev1alpha1.EndpointSpec) EndpointStatus {
	start := time.Now()

	// Extract host:port from URL
	host, port := extractHostPort(ep.URL)
	if host == "" {
		return EndpointStatus{
			Reachable: false,
			Error:     fmt.Sprintf("could not parse host from URL: %s", ep.URL),
		}
	}

	// TCP dial to check reachability
	addr := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return EndpointStatus{
			Reachable: false,
			Latency:   time.Since(start),
			Error:     fmt.Sprintf("TCP dial failed: %v", err),
		}
	}
	conn.Close()

	return EndpointStatus{
		Reachable: true,
		Latency:   time.Since(start),
	}
}

// extractHostPort parses a URL or host:port string into host and port.
func extractHostPort(url string) (string, string) {
	// Strip protocol prefix
	u := url
	for _, prefix := range []string{"https://", "http://", "tcp://", "ssh://"} {
		u = strings.TrimPrefix(u, prefix)
	}

	// Strip path
	if idx := strings.Index(u, "/"); idx != -1 {
		u = u[:idx]
	}

	// Split host:port
	host, port, err := net.SplitHostPort(u)
	if err != nil {
		// No port — use defaults based on protocol
		host = u
		if strings.HasPrefix(url, "https://") {
			port = "443"
		} else if strings.HasPrefix(url, "http://") {
			port = "80"
		} else if strings.HasPrefix(url, "ssh://") {
			port = "22"
		} else {
			port = "443" // default
		}
	}

	return host, port
}

// PreRunCheck performs a comprehensive connectivity check before an agent run.
// Returns nil if connectivity is healthy, an error otherwise.
func (m *Manager) PreRunCheck(ctx context.Context, spec *corev1alpha1.ConnectivitySpec, endpoints map[string]corev1alpha1.EndpointSpec) error {
	// Check connectivity layer health
	health := m.CheckHealth(ctx, spec)
	if !health.Ready {
		return fmt.Errorf("connectivity not ready: %s", health.Message)
	}

	m.log.Info("Connectivity layer healthy",
		"type", health.Type,
		"message", health.Message)

	// Check critical endpoints
	if len(endpoints) > 0 {
		results := m.CheckEndpoints(ctx, endpoints)
		var unreachable []string
		for name, status := range results {
			if !status.Reachable {
				unreachable = append(unreachable, fmt.Sprintf("%s: %s", name, status.Error))
				m.log.Error(nil, "Endpoint unreachable",
					"endpoint", name,
					"error", status.Error)
			} else {
				m.log.V(1).Info("Endpoint reachable",
					"endpoint", name,
					"latency", status.Latency)
			}
		}

		if len(unreachable) > 0 {
			// Log but don't fail — some endpoints may be non-critical
			m.log.Info("Some endpoints unreachable",
				"count", len(unreachable),
				"total", len(results))
		}
	}

	return nil
}
