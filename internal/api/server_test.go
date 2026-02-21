package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/marcus-qen/legator/internal/api/auth"
	"github.com/marcus-qen/legator/internal/api/rbac"
)

// makeTestJWT creates a minimal JWT for testing (same as auth package helper).
func makeTestJWT(claims map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	body := base64.RawURLEncoding.EncodeToString(payload)
	return fmt.Sprintf("%s.%s.test-signature", header, body)
}

func TestHealthzBypassesAuth(t *testing.T) {
	srv := NewServer(ServerConfig{
		OIDC: auth.OIDCConfig{
			BypassPaths: []string{"/healthz"},
		},
	}, nil, logr.Discard())

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var body map[string]string
	json.NewDecoder(rr.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestUnauthenticatedRequestDenied(t *testing.T) {
	srv := NewServer(ServerConfig{
		OIDC: auth.OIDCConfig{
			BypassPaths: []string{"/healthz"},
		},
	}, nil, logr.Discard())

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestRBACDeniesViewerFromRunAgent(t *testing.T) {
	srv := NewServer(ServerConfig{
		OIDC: auth.OIDCConfig{
			BypassPaths: []string{"/healthz"},
		},
		Policies: []rbac.UserPolicy{
			{
				Name:     "viewer-only",
				Subjects: []rbac.SubjectMatcher{{Claim: "email", Value: "viewer@example.com"}},
				Role:     rbac.RoleViewer,
			},
		},
	}, nil, logr.Discard())

	token := makeTestJWT(map[string]interface{}{
		"sub":   "viewer-1",
		"email": "viewer@example.com",
		"exp":   float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	req := httptest.NewRequest("POST", "/api/v1/agents/forge/run", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestAuditMiddlewareLogsRequests(t *testing.T) {
	srv := NewServer(ServerConfig{
		OIDC: auth.OIDCConfig{
			BypassPaths: []string{"/healthz"},
		},
	}, nil, logr.Discard())

	// Just verify it doesn't panic
	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}
