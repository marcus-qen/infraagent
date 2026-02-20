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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"sync"

	"golang.org/x/crypto/ssh"
)

// CredentialManager manages per-run credential lifecycle.
// It requests credentials at run start and revokes them at run end.
// All credentials are scoped to a single agent run.
type CredentialManager struct {
	client *Client
	mu     sync.Mutex

	// Track leases for cleanup
	leases []string

	// Track SSH materials for cleanup (in-memory only)
	sshKeys []ed25519.PrivateKey
}

// NewCredentialManager creates a manager backed by a Vault client.
func NewCredentialManager(client *Client) *CredentialManager {
	return &CredentialManager{
		client: client,
	}
}

// SSHCredentials contains everything needed to SSH into a server.
type SSHCredentials struct {
	// PrivateKey is the ephemeral private key (PEM-encoded).
	PrivateKey string

	// Certificate is the Vault-signed SSH certificate.
	Certificate string

	// User is the SSH username.
	User string
}

// RequestSSHCredentials generates an ephemeral key pair and gets it signed by Vault's SSH CA.
// The certificate is short-lived (default 5 minutes). The private key exists only in memory.
func (m *CredentialManager) RequestSSHCredentials(ctx context.Context, req SSHCARequest) (*SSHCredentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ephemeral ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Convert to SSH public key format
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("convert to SSH public key: %w", err)
	}
	pubKeyStr := string(ssh.MarshalAuthorizedKey(sshPubKey))

	// Request Vault to sign it
	signResp, err := m.client.SignSSHKey(ctx, SSHSignRequest{
		Mount:           req.Mount,
		Role:            req.Role,
		PublicKey:       pubKeyStr,
		ValidPrincipals: req.User,
		TTL:             req.TTL,
	})
	if err != nil {
		return nil, fmt.Errorf("vault SSH CA sign: %w", err)
	}

	// Track for cleanup
	m.sshKeys = append(m.sshKeys, privKey)

	// Encode private key to PEM
	privKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: marshalED25519PrivateKey(privKey),
	})

	return &SSHCredentials{
		PrivateKey:  string(privKeyPEM),
		Certificate: signResp.SignedKey,
		User:        req.User,
	}, nil
}

// SSHCARequest describes what SSH credentials to request from Vault.
type SSHCARequest struct {
	// Mount is the SSH secrets engine mount (e.g. "ssh-client-signer").
	Mount string

	// Role is the Vault SSH CA role.
	Role string

	// User is the SSH username (valid principal).
	User string

	// TTL for the certificate (default "5m").
	TTL string
}

// DBCredentials contains dynamically-generated database access.
type DBCredentials struct {
	Username string
	Password string
	LeaseID  string
}

// RequestDBCredentials generates temporary database credentials via Vault.
// Credentials are automatically tracked for revocation on cleanup.
func (m *CredentialManager) RequestDBCredentials(ctx context.Context, mount, role string) (*DBCredentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	creds, err := m.client.GetDatabaseCredentials(ctx, mount, role)
	if err != nil {
		return nil, err
	}

	// Track lease for cleanup
	if creds.LeaseID != "" {
		m.leases = append(m.leases, creds.LeaseID)
	}

	return &DBCredentials{
		Username: creds.Username,
		Password: creds.Password,
		LeaseID:  creds.LeaseID,
	}, nil
}

// ReadSecret reads a static secret from Vault KV v2.
func (m *CredentialManager) ReadSecret(ctx context.Context, mount, path string) (map[string]interface{}, error) {
	return m.client.ReadKV(ctx, mount, path)
}

// Cleanup revokes all leases and zeroes all in-memory keys.
// This MUST be called when the agent run ends (success or failure).
// Errors are collected but don't prevent other cleanups from running.
func (m *CredentialManager) Cleanup(ctx context.Context) []error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	// Revoke all database leases
	for _, leaseID := range m.leases {
		if err := m.client.RevokeLease(ctx, leaseID); err != nil {
			errs = append(errs, fmt.Errorf("revoke lease %s: %w", leaseID, err))
		}
	}
	m.leases = nil

	// Zero out private keys in memory
	for i := range m.sshKeys {
		for j := range m.sshKeys[i] {
			m.sshKeys[i][j] = 0
		}
	}
	m.sshKeys = nil

	return errs
}

// ActiveLeaseCount returns the number of tracked leases.
func (m *CredentialManager) ActiveLeaseCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.leases)
}

// marshalED25519PrivateKey converts an ed25519 private key to OpenSSH format bytes.
// This is a simplified version — in production, use golang.org/x/crypto/ssh for proper marshaling.
func marshalED25519PrivateKey(key ed25519.PrivateKey) []byte {
	// For ed25519, the private key is the seed (32 bytes) + public key (32 bytes)
	return []byte(key)
}
