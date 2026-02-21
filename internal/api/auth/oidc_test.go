package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

// makeJWT creates a minimal unsigned JWT for testing.
func makeJWT(claims map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	body := base64.RawURLEncoding.EncodeToString(payload)
	return fmt.Sprintf("%s.%s.test-signature", header, body)
}

func TestValidateToken_BasicClaims(t *testing.T) {
	v := NewValidator(OIDCConfig{
		IssuerURL: "https://keycloak.example.com/realms/test",
		Audience:  "legator",
	}, logr.Discard())

	token := makeJWT(map[string]interface{}{
		"iss":    "https://keycloak.example.com/realms/test",
		"aud":    "legator",
		"sub":    "user-123",
		"email":  "keith@example.com",
		"name":   "Keith",
		"groups": []interface{}{"admin", "sre-team"},
		"exp":    float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	user, err := v.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Subject != "user-123" {
		t.Errorf("subject = %q, want %q", user.Subject, "user-123")
	}
	if user.Email != "keith@example.com" {
		t.Errorf("email = %q, want %q", user.Email, "keith@example.com")
	}
	if user.Name != "Keith" {
		t.Errorf("name = %q, want %q", user.Name, "Keith")
	}
	if len(user.Groups) != 2 {
		t.Errorf("groups = %v, want 2 items", user.Groups)
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	v := NewValidator(OIDCConfig{}, logr.Discard())

	token := makeJWT(map[string]interface{}{
		"sub": "user-123",
		"exp": float64(time.Now().Add(-1 * time.Hour).Unix()),
	})

	_, err := v.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if err.Error() != "token expired" {
		t.Errorf("error = %q, want 'token expired'", err.Error())
	}
}

func TestValidateToken_WrongIssuer(t *testing.T) {
	v := NewValidator(OIDCConfig{
		IssuerURL: "https://expected.example.com",
	}, logr.Discard())

	token := makeJWT(map[string]interface{}{
		"iss": "https://wrong.example.com",
		"sub": "user-123",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	_, err := v.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestValidateToken_WrongAudience(t *testing.T) {
	v := NewValidator(OIDCConfig{
		Audience: "legator",
	}, logr.Discard())

	token := makeJWT(map[string]interface{}{
		"aud": "wrong-client",
		"sub": "user-123",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	_, err := v.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidateToken_MalformedJWT(t *testing.T) {
	v := NewValidator(OIDCConfig{}, logr.Discard())

	_, err := v.ValidateToken(context.Background(), "not-a-jwt")
	if err == nil {
		t.Fatal("expected error for malformed JWT")
	}
}

func TestMiddleware_NoAuthHeader(t *testing.T) {
	v := NewValidator(OIDCConfig{}, logr.Discard())

	handler := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_BypassPath(t *testing.T) {
	v := NewValidator(OIDCConfig{
		BypassPaths: []string{"/healthz", "/api/v1/auth/"},
	}, logr.Discard())

	called := false
	handler := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called for bypass path")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	v := NewValidator(OIDCConfig{}, logr.Discard())

	token := makeJWT(map[string]interface{}{
		"sub":   "user-123",
		"email": "keith@example.com",
		"exp":   float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	var capturedUser string
	handler := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user != nil {
			capturedUser = user.Email
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if capturedUser != "keith@example.com" {
		t.Errorf("user email = %q, want %q", capturedUser, "keith@example.com")
	}
}

func TestUserFromContext_NoUser(t *testing.T) {
	user := UserFromContext(context.Background())
	if user != nil {
		t.Error("expected nil for context without user")
	}
}
