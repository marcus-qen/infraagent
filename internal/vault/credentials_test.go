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
)

func TestCredentialManager_RequestDBCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/database/creds/readonly" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"lease_id":       "database/creds/readonly/lease-1",
				"lease_duration": 300,
				"data": map[string]interface{}{
					"username": "v-temp-user",
					"password": "temp-pass",
				},
			})
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
	}))
	defer srv.Close()

	client, _ := NewClient(Config{Address: srv.URL, Token: "test"})
	mgr := NewCredentialManager(client)

	creds, err := mgr.RequestDBCredentials(context.Background(), "database", "readonly")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "v-temp-user" {
		t.Errorf("unexpected username: %s", creds.Username)
	}
	if mgr.ActiveLeaseCount() != 1 {
		t.Errorf("expected 1 active lease, got %d", mgr.ActiveLeaseCount())
	}
}

func TestCredentialManager_Cleanup(t *testing.T) {
	revokedLeases := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/database/creds/role1":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"lease_id":       "lease-1",
				"lease_duration": 300,
				"data":           map[string]interface{}{"username": "u1", "password": "p1"},
			})
		case "/v1/database/creds/role2":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"lease_id":       "lease-2",
				"lease_duration": 300,
				"data":           map[string]interface{}{"username": "u2", "password": "p2"},
			})
		case "/v1/sys/leases/revoke":
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			revokedLeases = append(revokedLeases, body["lease_id"].(string))
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client, _ := NewClient(Config{Address: srv.URL, Token: "test"})
	mgr := NewCredentialManager(client)

	// Request two sets of credentials
	_, _ = mgr.RequestDBCredentials(context.Background(), "database", "role1")
	_, _ = mgr.RequestDBCredentials(context.Background(), "database", "role2")

	if mgr.ActiveLeaseCount() != 2 {
		t.Errorf("expected 2 active leases, got %d", mgr.ActiveLeaseCount())
	}

	// Cleanup
	errs := mgr.Cleanup(context.Background())
	if len(errs) > 0 {
		t.Fatalf("cleanup errors: %v", errs)
	}

	if len(revokedLeases) != 2 {
		t.Fatalf("expected 2 revocations, got %d", len(revokedLeases))
	}
	if mgr.ActiveLeaseCount() != 0 {
		t.Errorf("expected 0 active leases after cleanup, got %d", mgr.ActiveLeaseCount())
	}
}

func TestCredentialManager_Cleanup_ContinuesOnError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/database/creds/role1", "/v1/database/creds/role2":
			leaseID := "lease-1"
			if r.URL.Path == "/v1/database/creds/role2" {
				leaseID = "lease-2"
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"lease_id":       leaseID,
				"lease_duration": 300,
				"data":           map[string]interface{}{"username": "u", "password": "p"},
			})
		case "/v1/sys/leases/revoke":
			callCount++
			if callCount == 1 {
				// First revocation fails
				w.WriteHeader(500)
				w.Write([]byte(`{"errors":["internal error"]}`))
				return
			}
			// Second succeeds
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	client, _ := NewClient(Config{Address: srv.URL, Token: "test"})
	mgr := NewCredentialManager(client)

	_, _ = mgr.RequestDBCredentials(context.Background(), "database", "role1")
	_, _ = mgr.RequestDBCredentials(context.Background(), "database", "role2")

	errs := mgr.Cleanup(context.Background())
	// Should have attempted both revocations even though first failed
	if callCount != 2 {
		t.Errorf("expected 2 revocation attempts, got %d", callCount)
	}
	// Should report the error
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestCredentialManager_ReadSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"api_key": "sk-test-123",
				},
			},
		})
	}))
	defer srv.Close()

	client, _ := NewClient(Config{Address: srv.URL, Token: "test"})
	mgr := NewCredentialManager(client)

	data, err := mgr.ReadSecret(context.Background(), "secret", "myapp/keys")
	if err != nil {
		t.Fatal(err)
	}
	if data["api_key"] != "sk-test-123" {
		t.Errorf("unexpected api_key: %v", data["api_key"])
	}
}

func TestCredentialManager_RequestSSHCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"signed_key":    "ssh-ed25519-cert-v01@openssh.com AAAA...",
				"serial_number": "serial-123",
			},
		})
	}))
	defer srv.Close()

	client, _ := NewClient(Config{Address: srv.URL, Token: "test"})
	mgr := NewCredentialManager(client)

	creds, err := mgr.RequestSSHCredentials(context.Background(), SSHCARequest{
		Mount: "ssh-client-signer",
		Role:  "default",
		User:  "marcus",
		TTL:   "5m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if creds.Certificate == "" {
		t.Error("expected certificate")
	}
	if creds.PrivateKey == "" {
		t.Error("expected private key")
	}
	if creds.User != "marcus" {
		t.Errorf("expected user marcus, got %s", creds.User)
	}

	// Cleanup should zero the key
	errs := mgr.Cleanup(context.Background())
	if len(errs) > 0 {
		t.Errorf("cleanup errors: %v", errs)
	}
}
