/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/marcus-qen/legator/internal/api/rbac"
)

type contextKey string

const userContextKey contextKey = "legator-user"

// OIDCConfig configures the OIDC JWT validator.
type OIDCConfig struct {
	// IssuerURL is the OIDC issuer (e.g., https://keycloak.example.com/realms/dev-lab).
	IssuerURL string

	// Audience is the expected "aud" claim (client ID).
	Audience string

	// GroupsClaim is the JWT claim containing group memberships.
	GroupsClaim string

	// EmailClaim is the JWT claim containing the email.
	EmailClaim string

	// NameClaim is the JWT claim containing the display name.
	NameClaim string

	// BypassPaths are paths that don't require authentication.
	BypassPaths []string
}

// JWKSKeys represents a simplified JWKS response.
type JWKSKeys struct {
	Keys []json.RawMessage `json:"keys"`
}

// Validator validates OIDC JWTs and extracts user identity.
type Validator struct {
	config     OIDCConfig
	log        logr.Logger
	mu         sync.RWMutex
	jwksURL    string
	jwksCache  *JWKSKeys
	jwksCached time.Time
}

// NewValidator creates a new OIDC validator.
func NewValidator(config OIDCConfig, log logr.Logger) *Validator {
	if config.GroupsClaim == "" {
		config.GroupsClaim = "groups"
	}
	if config.EmailClaim == "" {
		config.EmailClaim = "email"
	}
	if config.NameClaim == "" {
		config.NameClaim = "name"
	}

	return &Validator{
		config: config,
		log:    log,
	}
}

// Middleware returns an HTTP middleware that validates OIDC JWTs.
func (v *Validator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check bypass paths
		for _, bp := range v.config.BypassPaths {
			if strings.HasPrefix(r.URL.Path, bp) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Extract Bearer token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"invalid authorization header format"}`, http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			http.Error(w, `{"error":"empty bearer token"}`, http.StatusUnauthorized)
			return
		}

		// Decode and validate JWT
		user, err := v.ValidateToken(r.Context(), token)
		if err != nil {
			v.log.Info("JWT validation failed", "error", err.Error(), "path", r.URL.Path)
			http.Error(w, fmt.Sprintf(`{"error":"invalid token: %s"}`, err.Error()), http.StatusUnauthorized)
			return
		}

		// Store user in context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ValidateToken decodes a JWT and extracts the user identity.
// NOTE: This performs claim validation (issuer, audience, expiry) but does NOT
// verify the cryptographic signature. Production deployments MUST use a proper
// JWKS-based verifier (e.g., coreos/go-oidc). This simplified version is
// sufficient for the v0.8.0 prototype where Pomerium already validates tokens.
func (v *Validator) ValidateToken(_ context.Context, token string) (*rbac.UserIdentity, error) {
	// Decode JWT payload (base64url-encoded, second segment)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed JWT: expected 3 parts, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	// Check issuer
	if iss, ok := claims["iss"].(string); ok {
		if v.config.IssuerURL != "" && iss != v.config.IssuerURL {
			return nil, fmt.Errorf("issuer mismatch: got %s, want %s", iss, v.config.IssuerURL)
		}
	}

	// Check audience
	if v.config.Audience != "" {
		if !checkAudience(claims, v.config.Audience) {
			return nil, fmt.Errorf("audience mismatch: expected %s", v.config.Audience)
		}
	}

	// Check expiry
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, fmt.Errorf("token expired")
		}
	}

	// Extract user identity
	user := &rbac.UserIdentity{
		Claims: claims,
	}

	if sub, ok := claims["sub"].(string); ok {
		user.Subject = sub
	}
	if email, ok := claims[v.config.EmailClaim].(string); ok {
		user.Email = email
	}
	if name, ok := claims[v.config.NameClaim].(string); ok {
		user.Name = name
	}

	// Extract groups (array of strings)
	if groups, ok := claims[v.config.GroupsClaim]; ok {
		switch g := groups.(type) {
		case []interface{}:
			for _, item := range g {
				if s, ok := item.(string); ok {
					user.Groups = append(user.Groups, s)
				}
			}
		case string:
			user.Groups = []string{g}
		}
	}

	return user, nil
}

// UserFromContext extracts the authenticated user from the request context.
func UserFromContext(ctx context.Context) *rbac.UserIdentity {
	if u, ok := ctx.Value(userContextKey).(*rbac.UserIdentity); ok {
		return u
	}
	return nil
}

// checkAudience verifies the "aud" claim matches the expected audience.
func checkAudience(claims map[string]interface{}, expected string) bool {
	aud, ok := claims["aud"]
	if !ok {
		return false
	}

	switch a := aud.(type) {
	case string:
		return a == expected
	case []interface{}:
		for _, v := range a {
			if s, ok := v.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}
