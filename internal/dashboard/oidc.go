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
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OIDCConfig holds OIDC provider configuration.
type OIDCConfig struct {
	// IssuerURL is the OIDC provider URL (e.g., https://keycloak.example.com/realms/legator).
	IssuerURL string

	// ClientID is the OIDC client ID.
	ClientID string

	// ClientSecret is the OIDC client secret.
	ClientSecret string

	// RedirectURL is the callback URL (e.g., https://legator.example.com/auth/callback).
	RedirectURL string

	// Scopes requested during auth (default: openid profile email).
	Scopes []string

	// SessionDuration is how long a session lasts (default: 8h).
	SessionDuration time.Duration

	// CookieName is the session cookie name (default: legator-session).
	CookieName string

	// CookieSecure forces Secure flag on cookie.
	CookieSecure bool
}

// OIDCUser holds the authenticated user info.
type OIDCUser struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	PreferredUser string `json:"preferred_username"`
	Groups        []string `json:"groups,omitempty"`
}

// OIDCMiddleware provides OIDC authentication for the dashboard.
type OIDCMiddleware struct {
	config   OIDCConfig
	sessions sync.Map // sessionID → *OIDCSession

	// OIDC discovery endpoints (fetched from .well-known)
	authEndpoint  string
	tokenEndpoint string
	userEndpoint  string
	discovered    bool
	mu            sync.Mutex
}

// OIDCSession holds an authenticated session.
type OIDCSession struct {
	User      OIDCUser
	ExpiresAt time.Time
	State     string // CSRF protection
}

// NewOIDCMiddleware creates the OIDC auth middleware.
func NewOIDCMiddleware(config OIDCConfig) *OIDCMiddleware {
	if config.CookieName == "" {
		config.CookieName = "legator-session"
	}
	if config.SessionDuration == 0 {
		config.SessionDuration = 8 * time.Hour
	}
	if len(config.Scopes) == 0 {
		config.Scopes = []string{"openid", "profile", "email"}
	}

	return &OIDCMiddleware{
		config: config,
	}
}

// Wrap protects a handler with OIDC authentication.
// Unauthenticated requests are redirected to the OIDC provider.
// Auth callback and static assets are excluded.
func (m *OIDCMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Auth callback — always allowed
		if path == "/auth/callback" {
			m.handleCallback(w, r, next)
			return
		}

		// Login route
		if path == "/auth/login" {
			m.handleLogin(w, r)
			return
		}

		// Logout route
		if path == "/auth/logout" {
			m.handleLogout(w, r)
			return
		}

		// Static assets — no auth required
		if strings.HasPrefix(path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		// Health check — no auth
		if path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
			return
		}

		// Check session cookie
		user := m.getSession(r)
		if user == nil {
			// Not authenticated — redirect to login
			m.handleLogin(w, r)
			return
		}

		// Inject user into context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleLogin redirects to the OIDC provider.
func (m *OIDCMiddleware) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := m.discover(); err != nil {
		http.Error(w, fmt.Sprintf("OIDC discovery failed: %v", err), http.StatusInternalServerError)
		return
	}

	state := generateState()

	// Store state for CSRF verification
	m.sessions.Store("state:"+state, &OIDCSession{
		State:     state,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	})

	scopes := strings.Join(m.config.Scopes, " ")
	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		m.authEndpoint,
		m.config.ClientID,
		m.config.RedirectURL,
		scopes,
		state,
	)

	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback processes the OIDC callback.
func (m *OIDCMiddleware) handleCallback(w http.ResponseWriter, r *http.Request, next http.Handler) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errParam := r.URL.Query().Get("error")

	if errParam != "" {
		desc := r.URL.Query().Get("error_description")
		http.Error(w, fmt.Sprintf("OIDC error: %s — %s", errParam, desc), http.StatusForbidden)
		return
	}

	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	// Verify state (CSRF)
	stateSession, ok := m.sessions.LoadAndDelete("state:" + state)
	if !ok {
		http.Error(w, "Invalid state — possible CSRF", http.StatusForbidden)
		return
	}
	ss := stateSession.(*OIDCSession)
	if time.Now().After(ss.ExpiresAt) {
		http.Error(w, "State expired", http.StatusForbidden)
		return
	}

	// Exchange code for token
	token, err := m.exchangeCode(code)
	if err != nil {
		http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Get user info
	user, err := m.getUserInfo(token)
	if err != nil {
		http.Error(w, fmt.Sprintf("User info failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Create session
	sessionID := generateState()
	m.sessions.Store(sessionID, &OIDCSession{
		User:      *user,
		ExpiresAt: time.Now().Add(m.config.SessionDuration),
	})

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     m.config.CookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(m.config.SessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   m.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to dashboard
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLogout clears the session and redirects to login.
func (m *OIDCMiddleware) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(m.config.CookieName)
	if err == nil {
		m.sessions.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   m.config.CookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

// getSession returns the user from a valid session cookie.
func (m *OIDCMiddleware) getSession(r *http.Request) *OIDCUser {
	cookie, err := r.Cookie(m.config.CookieName)
	if err != nil {
		return nil
	}

	session, ok := m.sessions.Load(cookie.Value)
	if !ok {
		return nil
	}

	s := session.(*OIDCSession)
	if time.Now().After(s.ExpiresAt) {
		m.sessions.Delete(cookie.Value)
		return nil
	}

	return &s.User
}

// --- OIDC Protocol ---

// OIDCDiscovery is the well-known OpenID configuration.
type OIDCDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	Issuer                string `json:"issuer"`
}

// discover fetches OIDC endpoints from .well-known.
func (m *OIDCMiddleware) discover() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.discovered {
		return nil
	}

	url := strings.TrimRight(m.config.IssuerURL, "/") + "/.well-known/openid-configuration"
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("discovery request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discovery returned %d: %s", resp.StatusCode, string(body))
	}

	var disc OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return fmt.Errorf("discovery parse: %w", err)
	}

	m.authEndpoint = disc.AuthorizationEndpoint
	m.tokenEndpoint = disc.TokenEndpoint
	m.userEndpoint = disc.UserinfoEndpoint
	m.discovered = true

	return nil
}

// TokenResponse is the OIDC token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

// exchangeCode trades an auth code for tokens.
func (m *OIDCMiddleware) exchangeCode(code string) (string, error) {
	data := fmt.Sprintf("grant_type=authorization_code&code=%s&redirect_uri=%s&client_id=%s&client_secret=%s",
		code, m.config.RedirectURL, m.config.ClientID, m.config.ClientSecret)

	resp, err := http.Post(m.tokenEndpoint, "application/x-www-form-urlencoded", strings.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("token parse: %w", err)
	}

	return token.AccessToken, nil
}

// getUserInfo fetches user profile from the userinfo endpoint.
func (m *OIDCMiddleware) getUserInfo(accessToken string) (*OIDCUser, error) {
	req, _ := http.NewRequest("GET", m.userEndpoint, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo returned %d: %s", resp.StatusCode, string(body))
	}

	var user OIDCUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("userinfo parse: %w", err)
	}

	return &user, nil
}

// --- Helpers ---

type contextKey string

const userContextKey contextKey = "oidc-user"

// UserFromContext extracts the authenticated user from request context.
func UserFromContext(ctx context.Context) *OIDCUser {
	user, _ := ctx.Value(userContextKey).(*OIDCUser)
	return user
}

// generateState creates a random state parameter for CSRF protection.
func generateState() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
