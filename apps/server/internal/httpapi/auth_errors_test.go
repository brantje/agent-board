package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAuthHTTPRejectsMalformedRequests(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)

	for _, path := range []string{
		"/api/auth/bootstrap/register",
		"/api/auth/login",
		"/api/auth/refresh",
		"/api/auth/logout",
		"/api/auth/setup/complete",
		"/api/auth/reset/complete",
	} {
		response := authHTTPRequest(t, handler, http.MethodPost, path, `{`, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s malformed JSON status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestAuthHTTPMapsAuthenticationAndBootstrapFailures(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)

	secondBootstrap := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/bootstrap/register", `{"username":"second","email":"second@example.com","displayName":"Second","password":"long-enough-password"}`, nil)
	if secondBootstrap.Code != http.StatusConflict || !strings.Contains(secondBootstrap.Body.String(), "bootstrap_closed") {
		t.Fatalf("second bootstrap status=%d body=%s", secondBootstrap.Code, secondBootstrap.Body.String())
	}

	wrongPassword := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"admin","password":"wrong-password-value"}`, nil)
	if wrongPassword.Code != http.StatusUnauthorized || !strings.Contains(wrongPassword.Body.String(), "authentication_failed") || strings.Contains(wrongPassword.Body.String(), "wrong-password-value") {
		t.Fatalf("wrong-password response status=%d body=%s", wrongPassword.Code, wrongPassword.Body.String())
	}

	unknownUser := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"missing@example.com","password":"long-enough-password"}`, nil)
	if unknownUser.Code != http.StatusUnauthorized || !strings.Contains(unknownUser.Body.String(), "authentication_failed") {
		t.Fatalf("unknown-user response status=%d body=%s", unknownUser.Code, unknownUser.Body.String())
	}

	invalidRefresh := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", `{"refreshToken":"not-a-token"}`, nil)
	if invalidRefresh.Code != http.StatusUnauthorized || !strings.Contains(invalidRefresh.Body.String(), "authentication_failed") {
		t.Fatalf("invalid-refresh response status=%d body=%s", invalidRefresh.Code, invalidRefresh.Body.String())
	}
}

func TestAuthHTTPLogoutAndCurrentUserRequireCredentials(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)

	missingRefresh := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/logout", `{"refreshToken":"   "}`, nil)
	if missingRefresh.Code != http.StatusBadRequest || !strings.Contains(missingRefresh.Body.String(), "invalid_request") {
		t.Fatalf("missing refresh status=%d body=%s", missingRefresh.Code, missingRefresh.Body.String())
	}

	invalidLogout := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/logout", `{"refreshToken":"not-a-token"}`, nil)
	if invalidLogout.Code != http.StatusNoContent {
		t.Fatalf("idempotent invalid logout status=%d body=%s", invalidLogout.Code, invalidLogout.Body.String())
	}

	for _, authorization := range []string{"", "Basic abc", "Bearer", "Bearer one two"} {
		headers := map[string]string{}
		if authorization != "" {
			headers["Authorization"] = authorization
		}
		response := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", headers)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization %q status=%d body=%s", authorization, response.Code, response.Body.String())
		}
	}

	invalidBearer := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{"Authorization": "Bearer not-a-jwt"})
	if invalidBearer.Code != http.StatusUnauthorized || !strings.Contains(invalidBearer.Body.String(), "authentication_failed") {
		t.Fatalf("invalid bearer status=%d body=%s", invalidBearer.Code, invalidBearer.Body.String())
	}
}

func TestAuthHTTPPasswordTokenFailuresAreSafe(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)

	for _, path := range []string{"/api/auth/setup/complete", "/api/auth/reset/complete"} {
		secret := "definitely-not-a-real-token"
		password := "long-enough-password"
		response := authHTTPRequest(t, handler, http.MethodPost, path, fmt.Sprintf(`{"token":%q,"password":%q}`, secret, password), nil)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "password_token_invalid") {
			t.Fatalf("%s invalid token status=%d body=%s", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), password) {
			t.Fatalf("%s leaked credential material: %s", path, response.Body.String())
		}
	}
}
