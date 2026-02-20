/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package connectivity

import (
	"context"
	"testing"

	"github.com/go-logr/logr"

	corev1alpha1 "github.com/marcus-qen/legator/api/v1alpha1"
)

func TestCheckHealth_NilSpec(t *testing.T) {
	mgr := NewManager(logr.Discard())
	status := mgr.CheckHealth(context.Background(), nil)

	if !status.Ready {
		t.Error("nil spec should be ready (direct connectivity)")
	}
	if status.Type != "direct" {
		t.Errorf("expected type 'direct', got %s", status.Type)
	}
}

func TestCheckHealth_Direct(t *testing.T) {
	mgr := NewManager(logr.Discard())
	spec := &corev1alpha1.ConnectivitySpec{Type: "direct"}
	status := mgr.CheckHealth(context.Background(), spec)

	if !status.Ready {
		t.Error("direct connectivity should be ready")
	}
	if status.Type != "direct" {
		t.Errorf("expected type 'direct', got %s", status.Type)
	}
}

func TestCheckHealth_UnknownType(t *testing.T) {
	mgr := NewManager(logr.Discard())
	spec := &corev1alpha1.ConnectivitySpec{Type: "quantum-entanglement"}
	status := mgr.CheckHealth(context.Background(), spec)

	if status.Ready {
		t.Error("unknown type should not be ready")
	}
}

func TestCheckHealth_HeadscaleNotRunning(t *testing.T) {
	mgr := NewManager(logr.Discard())
	spec := &corev1alpha1.ConnectivitySpec{
		Type: "headscale",
		Headscale: &corev1alpha1.HeadscaleConnectivity{
			ControlServer:    "https://headscale.example.com",
			AuthKeySecretRef: "headscale-key",
		},
	}

	// In test environment, no Tailscale sidecar is running
	status := mgr.CheckHealth(context.Background(), spec)
	if status.Ready {
		t.Error("headscale should not be ready without sidecar")
	}
	if status.Type != "headscale" {
		t.Errorf("expected type 'headscale', got %s", status.Type)
	}
}

func TestCheckHealth_TailscaleNotRunning(t *testing.T) {
	mgr := NewManager(logr.Discard())
	spec := &corev1alpha1.ConnectivitySpec{
		Type: "tailscale",
		Headscale: &corev1alpha1.HeadscaleConnectivity{
			ControlServer:    "https://controlplane.tailscale.com",
			AuthKeySecretRef: "tailscale-key",
			Tags:             []string{"tag:agent-runtime"},
		},
	}

	status := mgr.CheckHealth(context.Background(), spec)
	if status.Ready {
		t.Error("tailscale should not be ready without sidecar")
	}
}

func TestExtractHostPort(t *testing.T) {
	tests := []struct {
		url      string
		wantHost string
		wantPort string
	}{
		{"https://grafana.monitoring:3000", "grafana.monitoring", "3000"},
		{"http://prometheus.monitoring:9090", "prometheus.monitoring", "9090"},
		{"https://api.example.com", "api.example.com", "443"},
		{"http://internal-service", "internal-service", "80"},
		{"ssh://centos7-proxy-01", "centos7-proxy-01", "22"},
		{"postgres.db-system:5432", "postgres.db-system", "5432"},
		{"10.20.5.100:3306", "10.20.5.100", "3306"},
		{"https://grafana.lab.k-dev.uk/d/abc123", "grafana.lab.k-dev.uk", "443"},
		{"tcp://rabbitmq.messaging:5672", "rabbitmq.messaging", "5672"},
	}

	for _, tt := range tests {
		host, port := extractHostPort(tt.url)
		if host != tt.wantHost {
			t.Errorf("extractHostPort(%q) host = %q, want %q", tt.url, host, tt.wantHost)
		}
		if port != tt.wantPort {
			t.Errorf("extractHostPort(%q) port = %q, want %q", tt.url, port, tt.wantPort)
		}
	}
}

func TestCheckEndpoints_Unreachable(t *testing.T) {
	mgr := NewManager(logr.Discard())

	endpoints := map[string]corev1alpha1.EndpointSpec{
		"nonexistent": {
			URL: "http://192.0.2.1:9999", // RFC 5737 TEST-NET — guaranteed unreachable
		},
	}

	results := mgr.CheckEndpoints(context.Background(), endpoints)

	status, ok := results["nonexistent"]
	if !ok {
		t.Fatal("expected 'nonexistent' in results")
	}
	if status.Reachable {
		t.Error("192.0.2.1:9999 should not be reachable")
	}
	if status.Error == "" {
		t.Error("expected error message for unreachable endpoint")
	}
}

func TestCheckEndpoints_EmptyMap(t *testing.T) {
	mgr := NewManager(logr.Discard())
	results := mgr.CheckEndpoints(context.Background(), map[string]corev1alpha1.EndpointSpec{})

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestPreRunCheck_DirectNoEndpoints(t *testing.T) {
	mgr := NewManager(logr.Discard())

	err := mgr.PreRunCheck(context.Background(), nil, nil)
	if err != nil {
		t.Errorf("direct connectivity with no endpoints should pass: %v", err)
	}
}

func TestPreRunCheck_HeadscaleNotReady(t *testing.T) {
	mgr := NewManager(logr.Discard())
	spec := &corev1alpha1.ConnectivitySpec{
		Type: "headscale",
		Headscale: &corev1alpha1.HeadscaleConnectivity{
			ControlServer:    "https://headscale.example.com",
			AuthKeySecretRef: "headscale-key",
		},
	}

	err := mgr.PreRunCheck(context.Background(), spec, nil)
	if err == nil {
		t.Error("expected error when headscale sidecar not running")
	}
}

func TestNewManager(t *testing.T) {
	mgr := NewManager(logr.Discard())
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestExtractHostPort_BadURL(t *testing.T) {
	host, _ := extractHostPort("")
	if host != "" {
		t.Errorf("empty URL should return empty host, got %q", host)
	}
}
