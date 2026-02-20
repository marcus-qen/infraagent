/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewOIDCMiddleware_Defaults(t *testing.T) {
	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL:    "https://keycloak.example.com/realms/legator",
		ClientID:     "legator-dashboard",
		ClientSecret: "test-secret",
		RedirectURL:  "https://legator.example.com/auth/callback",
	})

	if m.config.CookieName != "legator-session" {
		t.Errorf("cookie name = %q, want legator-session", m.config.CookieName)
	}
	if m.config.SessionDuration != 8*time.Hour {
		t.Errorf("session duration = %v, want 8h", m.config.SessionDuration)
	}
	if len(m.config.Scopes) != 3 {
		t.Errorf("scopes = %v, want [openid profile email]", m.config.Scopes)
	}
}

func TestOIDCMiddleware_UnauthRedirectsToLogin(t *testing.T) {
	// Mock OIDC discovery
	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			json.NewEncoder(w).Encode(OIDCDiscovery{
				AuthorizationEndpoint: "https://keycloak.example.com/auth",
				TokenEndpoint:         "https://keycloak.example.com/token",
				UserinfoEndpoint:      "https://keycloak.example.com/userinfo",
				Issuer:                "https://keycloak.example.com",
			})
			return
		}
	}))
	defer oidcServer.Close()

	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL:    oidcServer.URL,
		ClientID:     "test",
		ClientSecret: "secret",
		RedirectURL:  "http://localhost/auth/callback",
	})

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("protected"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusFound)
	}

	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header")
	}
	// Should redirect to keycloak auth endpoint
	if len(loc) < 10 {
		t.Errorf("location too short: %q", loc)
	}
}

func TestOIDCMiddleware_StaticBypassesAuth(t *testing.T) {
	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL: "https://keycloak.example.com",
		ClientID:  "test",
	})

	called := false
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte("static"))
	}))

	req := httptest.NewRequest("GET", "/static/style.css", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected static handler to be called without auth")
	}
}

func TestOIDCMiddleware_HealthBypassesAuth(t *testing.T) {
	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL: "https://keycloak.example.com",
		ClientID:  "test",
	})

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach inner handler for healthz")
	}))

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestOIDCMiddleware_ValidSessionPassesThrough(t *testing.T) {
	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL: "https://keycloak.example.com",
		ClientID:  "test",
	})

	// Plant a session
	m.sessions.Store("test-session-id", &OIDCSession{
		User: OIDCUser{
			Subject:       "user-123",
			Email:         "keith@example.com",
			Name:          "Keith",
			PreferredUser: "keith",
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	var gotUser *OIDCUser
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "legator-session", Value: "test-session-id"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotUser == nil {
		t.Fatal("expected user in context")
	}
	if gotUser.Email != "keith@example.com" {
		t.Errorf("email = %q, want keith@example.com", gotUser.Email)
	}
}

func TestOIDCMiddleware_ExpiredSession(t *testing.T) {
	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(OIDCDiscovery{
			AuthorizationEndpoint: "https://keycloak.example.com/auth",
			TokenEndpoint:         "https://keycloak.example.com/token",
			UserinfoEndpoint:      "https://keycloak.example.com/userinfo",
		})
	}))
	defer oidcServer.Close()

	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL:   oidcServer.URL,
		ClientID:    "test",
		RedirectURL: "http://localhost/auth/callback",
	})

	// Plant an expired session
	m.sessions.Store("expired", &OIDCSession{
		User:      OIDCUser{Email: "old@example.com"},
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	})

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach inner handler")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "legator-session", Value: "expired"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expired session should redirect, got %d", rec.Code)
	}
}

func TestOIDCMiddleware_CallbackRejectsInvalidState(t *testing.T) {
	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL: "https://keycloak.example.com",
		ClientID:  "test",
	})

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/auth/callback?code=abc&state=bad-state", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("bad state should return 403, got %d", rec.Code)
	}
}

func TestOIDCMiddleware_CallbackWithError(t *testing.T) {
	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL: "https://keycloak.example.com",
		ClientID:  "test",
	})

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/auth/callback?error=access_denied&error_description=User+denied", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("OIDC error should return 403, got %d", rec.Code)
	}
}

func TestOIDCMiddleware_Logout(t *testing.T) {
	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL: "https://keycloak.example.com",
		ClientID:  "test",
	})

	m.sessions.Store("logout-test", &OIDCSession{
		User:      OIDCUser{Email: "test@example.com"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "legator-session", Value: "logout-test"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("logout should redirect, got %d", rec.Code)
	}

	// Session should be deleted
	if _, ok := m.sessions.Load("logout-test"); ok {
		t.Error("session should be deleted after logout")
	}
}

func TestOIDCMiddleware_FullFlow(t *testing.T) {
	// Set up mock OIDC provider
	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(OIDCDiscovery{
				AuthorizationEndpoint: fmt.Sprintf("http://%s/auth", r.Host),
				TokenEndpoint:         fmt.Sprintf("http://%s/token", r.Host),
				UserinfoEndpoint:      fmt.Sprintf("http://%s/userinfo", r.Host),
			})
		case "/token":
			json.NewEncoder(w).Encode(TokenResponse{
				AccessToken: "test-access-token",
				TokenType:   "Bearer",
			})
		case "/userinfo":
			json.NewEncoder(w).Encode(OIDCUser{
				Subject:       "keith-123",
				Email:         "keith@k-dev.uk",
				Name:          "Keith",
				PreferredUser: "keith",
				Groups:        []string{"platform-admins"},
			})
		}
	}))
	defer oidcServer.Close()

	m := NewOIDCMiddleware(OIDCConfig{
		IssuerURL:    oidcServer.URL,
		ClientID:     "legator-dashboard",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/auth/callback",
	})

	var capturedUser *OIDCUser
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromContext(r.Context())
		w.Write([]byte("dashboard"))
	}))

	// Step 1: Unauthenticated request → redirect to login
	req1 := httptest.NewRequest("GET", "/", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusFound {
		t.Fatalf("step 1: expected redirect, got %d", rec1.Code)
	}

	// Extract state from redirect URL
	loc := rec1.Header().Get("Location")
	// Parse state parameter
	stateIdx := len(loc) - 1
	for stateIdx > 0 && loc[stateIdx-1] != '=' {
		stateIdx--
	}
	state := loc[stateIdx:]

	// Step 2: Callback with code + valid state → creates session
	req2 := httptest.NewRequest("GET", fmt.Sprintf("/auth/callback?code=test-code&state=%s", state), nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusFound {
		t.Fatalf("step 2: expected redirect to /, got %d", rec2.Code)
	}

	// Extract session cookie
	cookies := rec2.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "legator-session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("step 2: no session cookie set")
	}

	// Step 3: Authenticated request with session cookie → passes through
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.AddCookie(sessionCookie)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("step 3: expected 200, got %d", rec3.Code)
	}
	if capturedUser == nil {
		t.Fatal("step 3: no user in context")
	}
	if capturedUser.Email != "keith@k-dev.uk" {
		t.Errorf("step 3: email = %q, want keith@k-dev.uk", capturedUser.Email)
	}
	if capturedUser.PreferredUser != "keith" {
		t.Errorf("step 3: preferred_username = %q, want keith", capturedUser.PreferredUser)
	}
}

func TestUserFromContext_NoUser(t *testing.T) {
	user := UserFromContext(context.Background())
	if user != nil {
		t.Error("expected nil user from empty context")
	}
}

func TestGenerateState(t *testing.T) {
	s1 := generateState()
	s2 := generateState()

	if len(s1) < 20 {
		t.Errorf("state too short: %q", s1)
	}
	if s1 == s2 {
		t.Error("states should be unique")
	}
}
