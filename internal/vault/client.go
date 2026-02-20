/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// Client provides authenticated access to HashiCorp Vault.
// It supports K8s auth, token auth, and manages token lifecycle.
type Client struct {
	addr       string
	httpClient *http.Client
	token      string
	mu         sync.RWMutex

	// K8s auth config (optional)
	k8sAuthRole string
	k8sAuthPath string
	saTokenPath string
}

// Config holds Vault client configuration.
type Config struct {
	// Address is the Vault server URL (e.g. "http://vault.example.com:8200").
	Address string

	// Token is a static Vault token. Mutually exclusive with K8s auth.
	Token string

	// K8sAuthRole is the Vault K8s auth role name.
	K8sAuthRole string

	// K8sAuthPath is the Vault auth mount path (default "kubernetes").
	K8sAuthPath string

	// SATokenPath is the path to the service account token (default "/var/run/secrets/kubernetes.io/serviceaccount/token").
	SATokenPath string

	// Timeout for HTTP requests (default 10s).
	Timeout time.Duration
}

// NewClient creates a Vault client from config.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("vault address is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	c := &Client{
		addr: cfg.Address,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		token:       cfg.Token,
		k8sAuthRole: cfg.K8sAuthRole,
		k8sAuthPath: cfg.K8sAuthPath,
		saTokenPath: cfg.SATokenPath,
	}

	if c.k8sAuthPath == "" {
		c.k8sAuthPath = "kubernetes"
	}
	if c.saTokenPath == "" {
		c.saTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	return c, nil
}

// Authenticate obtains a Vault token via K8s auth method.
// If a static token was provided, this is a no-op.
func (c *Client) Authenticate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Static token — nothing to do
	if c.token != "" && c.k8sAuthRole == "" {
		return nil
	}

	if c.k8sAuthRole == "" {
		return fmt.Errorf("neither token nor k8s auth role configured")
	}

	// Read service account token
	saToken, err := os.ReadFile(c.saTokenPath)
	if err != nil {
		return fmt.Errorf("failed to read SA token from %s: %w", c.saTokenPath, err)
	}

	// Call Vault K8s auth
	body := map[string]string{
		"role": c.k8sAuthRole,
		"jwt":  string(saToken),
	}
	bodyJSON, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/v1/auth/%s/login", c.addr, c.k8sAuthPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vault auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("vault auth failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var authResp vaultAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("failed to decode auth response: %w", err)
	}

	c.token = authResp.Auth.ClientToken
	return nil
}

// Health checks if Vault is reachable and unsealed.
func (c *Client) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/sys/health", c.addr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vault unreachable: %w", err)
	}
	defer resp.Body.Close()

	// 200 = initialized, unsealed, active
	// 429 = unsealed, standby
	// 472 = recovery mode
	// 473 = performance standby
	// 501 = not initialized
	// 503 = sealed
	switch resp.StatusCode {
	case 200, 429, 473:
		return nil
	case 501:
		return fmt.Errorf("vault not initialized")
	case 503:
		return fmt.Errorf("vault is sealed")
	default:
		return fmt.Errorf("vault health check returned status %d", resp.StatusCode)
	}
}

// ReadKV reads a secret from Vault KV v2 engine.
func (c *Client) ReadKV(ctx context.Context, mount, path string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/v1/%s/data/%s", c.addr, mount, path)
	data, err := c.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("read KV %s/%s: %w", mount, path, err)
	}

	// KV v2 wraps data in data.data
	dataField, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected KV response structure")
	}
	innerData, ok := dataField["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected KV data structure")
	}

	return innerData, nil
}

// --- SSH CA ---

// SSHSignRequest describes a request to sign an SSH public key.
type SSHSignRequest struct {
	// Mount is the SSH secrets engine mount path (e.g. "ssh-client-signer").
	Mount string

	// Role is the Vault SSH CA role name.
	Role string

	// PublicKey is the SSH public key to sign.
	PublicKey string

	// ValidPrincipals are the usernames the cert is valid for.
	ValidPrincipals string

	// TTL for the certificate (e.g. "5m").
	TTL string

	// CertType is "user" or "host" (default "user").
	CertType string
}

// SSHSignResponse contains the signed certificate.
type SSHSignResponse struct {
	// SignedKey is the signed SSH certificate.
	SignedKey string

	// SerialNumber is the certificate serial.
	SerialNumber string
}

// SignSSHKey requests Vault to sign an SSH public key via the SSH CA.
func (c *Client) SignSSHKey(ctx context.Context, req SSHSignRequest) (*SSHSignResponse, error) {
	if req.TTL == "" {
		req.TTL = "5m"
	}
	if req.CertType == "" {
		req.CertType = "user"
	}

	body := map[string]interface{}{
		"public_key":       req.PublicKey,
		"valid_principals": req.ValidPrincipals,
		"ttl":              req.TTL,
		"cert_type":        req.CertType,
	}

	url := fmt.Sprintf("%s/v1/%s/sign/%s", c.addr, req.Mount, req.Role)
	data, err := c.doPost(ctx, url, body)
	if err != nil {
		return nil, fmt.Errorf("sign SSH key: %w", err)
	}

	dataField, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected SSH sign response structure")
	}

	signedKey, _ := dataField["signed_key"].(string)
	serialNumber, _ := dataField["serial_number"].(string)

	if signedKey == "" {
		return nil, fmt.Errorf("empty signed key in response")
	}

	return &SSHSignResponse{
		SignedKey:     signedKey,
		SerialNumber: serialNumber,
	}, nil
}

// --- Dynamic Database Credentials ---

// DatabaseCredentials contains a dynamically-generated database username and password.
type DatabaseCredentials struct {
	Username string
	Password string
	LeaseID  string
	LeaseTTL time.Duration
}

// GetDatabaseCredentials generates temporary database credentials from a Vault database role.
func (c *Client) GetDatabaseCredentials(ctx context.Context, mount, role string) (*DatabaseCredentials, error) {
	if mount == "" {
		mount = "database"
	}

	url := fmt.Sprintf("%s/v1/%s/creds/%s", c.addr, mount, role)
	data, err := c.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get database creds for role %q: %w", role, err)
	}

	dataField, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected database creds response structure")
	}

	username, _ := dataField["username"].(string)
	password, _ := dataField["password"].(string)
	leaseID, _ := data["lease_id"].(string)
	leaseDuration, _ := data["lease_duration"].(float64)

	if username == "" || password == "" {
		return nil, fmt.Errorf("incomplete database credentials in response")
	}

	return &DatabaseCredentials{
		Username: username,
		Password: password,
		LeaseID:  leaseID,
		LeaseTTL: time.Duration(leaseDuration) * time.Second,
	}, nil
}

// RevokeLease explicitly revokes a Vault lease (e.g. database credentials).
func (c *Client) RevokeLease(ctx context.Context, leaseID string) error {
	if leaseID == "" {
		return nil
	}

	body := map[string]interface{}{
		"lease_id": leaseID,
	}
	url := fmt.Sprintf("%s/v1/sys/leases/revoke", c.addr)
	_, err := c.doPost(ctx, url, body)
	if err != nil {
		return fmt.Errorf("revoke lease %q: %w", leaseID, err)
	}
	return nil
}

// --- HTTP helpers ---

func (c *Client) doGet(ctx context.Context, url string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return c.doRequest(req)
}

func (c *Client) doPost(ctx context.Context, url string, body interface{}) (map[string]interface{}, error) {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req)
}

func (c *Client) doRequest(req *http.Request) (map[string]interface{}, error) {
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()

	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vault returned status %d: %s", resp.StatusCode, truncate(string(respBody), 256))
	}

	// Handle empty responses (e.g. 204 No Content from lease revocation)
	if len(respBody) == 0 {
		return map[string]interface{}{}, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return result, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// --- Response types ---

type vaultAuthResponse struct {
	Auth struct {
		ClientToken   string `json:"client_token"`
		LeaseDuration int    `json:"lease_duration"`
		Renewable     bool   `json:"renewable"`
	} `json:"auth"`
}
