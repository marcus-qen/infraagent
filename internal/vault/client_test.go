/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package vault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient_RequiresAddress(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error for empty address")
	}
}

func TestNewClient_Defaults(t *testing.T) {
	c, err := NewClient(Config{Address: "http://localhost:8200"})
	if err != nil {
		t.Fatal(err)
	}
	if c.k8sAuthPath != "kubernetes" {
		t.Errorf("expected default k8sAuthPath 'kubernetes', got %q", c.k8sAuthPath)
	}
	if c.saTokenPath != "/var/run/secrets/kubernetes.io/serviceaccount/token" {
		t.Errorf("unexpected default SA token path: %q", c.saTokenPath)
	}
}

func TestHealth_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sys/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"initialized":true,"sealed":false}`))
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL})
	err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealth_Sealed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL})
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected error for sealed vault")
	}
	if err.Error() != "vault is sealed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHealth_NotInitialized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(501)
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL})
	err := c.Health(context.Background())
	if err == nil || err.Error() != "vault not initialized" {
		t.Errorf("expected 'not initialized', got: %v", err)
	}
}

func TestHealth_Standby(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL})
	err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("standby should be healthy: %v", err)
	}
}

func TestReadKV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/myapp/config" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Vault-Token") != "test-token" {
			t.Errorf("missing or wrong token: %s", r.Header.Get("X-Vault-Token"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"username": "admin",
					"password": "secret123",
				},
				"metadata": map[string]interface{}{
					"version": 1,
				},
			},
		})
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL, Token: "test-token"})
	data, err := c.ReadKV(context.Background(), "secret", "myapp/config")
	if err != nil {
		t.Fatal(err)
	}
	if data["username"] != "admin" {
		t.Errorf("expected admin, got %v", data["username"])
	}
	if data["password"] != "secret123" {
		t.Errorf("expected secret123, got %v", data["password"])
	}
}

func TestSignSSHKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ssh-client-signer/sign/default" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["ttl"] != "5m" {
			t.Errorf("expected 5m TTL, got %v", body["ttl"])
		}
		if body["valid_principals"] != "marcus" {
			t.Errorf("expected marcus principal, got %v", body["valid_principals"])
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"signed_key":    "ssh-ed25519-cert-v01@openssh.com AAAA...",
				"serial_number": "abc123",
			},
		})
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL, Token: "test-token"})
	resp, err := c.SignSSHKey(context.Background(), SSHSignRequest{
		Mount:           "ssh-client-signer",
		Role:            "default",
		PublicKey:       "ssh-ed25519 AAAA...",
		ValidPrincipals: "marcus",
		TTL:             "5m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SignedKey == "" {
		t.Error("expected signed key")
	}
	if resp.SerialNumber != "abc123" {
		t.Errorf("expected serial abc123, got %s", resp.SerialNumber)
	}
}

func TestGetDatabaseCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/database/creds/readonly" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"lease_id":       "database/creds/readonly/abc123",
			"lease_duration": 3600,
			"data": map[string]interface{}{
				"username": "v-legator-readonly-abc123",
				"password": "temp-pass-xyz",
			},
		})
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL, Token: "test-token"})
	creds, err := c.GetDatabaseCredentials(context.Background(), "database", "readonly")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "v-legator-readonly-abc123" {
		t.Errorf("unexpected username: %s", creds.Username)
	}
	if creds.Password != "temp-pass-xyz" {
		t.Errorf("unexpected password: %s", creds.Password)
	}
	if creds.LeaseID != "database/creds/readonly/abc123" {
		t.Errorf("unexpected lease ID: %s", creds.LeaseID)
	}
	if creds.LeaseTTL != 3600*time.Second {
		t.Errorf("unexpected TTL: %v", creds.LeaseTTL)
	}
}

func TestRevokeLease(t *testing.T) {
	revoked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sys/leases/revoke" {
			revoked = true
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			if body["lease_id"] != "test-lease-123" {
				t.Errorf("unexpected lease_id: %v", body["lease_id"])
			}
			w.WriteHeader(204)
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL, Token: "test-token"})
	err := c.RevokeLease(context.Background(), "test-lease-123")
	if err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Error("lease was not revoked")
	}
}

func TestRevokeLease_EmptyID(t *testing.T) {
	c, _ := NewClient(Config{Address: "http://localhost:8200", Token: "test"})
	err := c.RevokeLease(context.Background(), "")
	if err != nil {
		t.Fatal("empty lease ID should be no-op")
	}
}

func TestAuthenticate_StaticToken(t *testing.T) {
	c, _ := NewClient(Config{Address: "http://localhost:8200", Token: "my-static-token"})
	err := c.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("static token auth should be no-op: %v", err)
	}
}

func TestAuthenticate_NoConfig(t *testing.T) {
	c, _ := NewClient(Config{Address: "http://localhost:8200"})
	err := c.Authenticate(context.Background())
	if err == nil {
		t.Fatal("expected error with no auth config")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Error("short string should not be truncated")
	}
	if truncate("this is a long string", 10) != "this is a ..." {
		t.Errorf("unexpected truncation: %q", truncate("this is a long string", 10))
	}
}

func TestReadKV_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"errors":["permission denied"]}`))
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Address: srv.URL, Token: "bad-token"})
	_, err := c.ReadKV(context.Background(), "secret", "forbidden")
	if err == nil {
		t.Fatal("expected error for 403")
	}
}
