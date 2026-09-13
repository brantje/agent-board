package httpapi

import (
	"net/http"
	"testing"
	"time"
)

func TestAuthPhase2HTTPValidationAndConflictBranches(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/auth/users"},
		{http.MethodPatch, "/api/auth/me"},
		{http.MethodPut, "/api/auth/me/password"},
		{http.MethodPost, "/api/auth/me/sessions/logout-others"},
		{http.MethodPut, "/api/auth/settings"},
	} {
		response := authHTTPRequest(t, handler, request.method, request.path, `{`, headers)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("malformed %s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
	}

	created := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"member","email":"member@example.com","displayName":"Member"}`, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create pending status=%d body=%s", created.Code, created.Body.String())
	}
	duplicate := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"member","email":"other@example.com","displayName":"Duplicate"}`, headers)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate user status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	profileConflict := authHTTPRequest(t, handler, http.MethodPatch, "/api/auth/me", `{"username":"member@example.com","email":"admin2@example.com","displayName":"Admin Two"}`, headers)
	if profileConflict.Code != http.StatusConflict {
		t.Fatalf("profile conflict status=%d body=%s", profileConflict.Code, profileConflict.Body.String())
	}

	invalidSession := authHTTPRequest(t, handler, http.MethodDelete, "/api/auth/me/sessions/not-a-uuid", "", headers)
	if invalidSession.Code != http.StatusBadRequest {
		t.Fatalf("invalid session id status=%d body=%s", invalidSession.Code, invalidSession.Body.String())
	}

	invalidSettingsJSON := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/settings", `{"accessTokenLifetimeSeconds":7200}`, headers)
	if invalidSettingsJSON.Code != http.StatusBadRequest {
		t.Fatalf("partial settings status=%d body=%s", invalidSettingsJSON.Code, invalidSettingsJSON.Body.String())
	}
}

func TestAuthPhase2OwnPasswordHTTPPolicyAndInvalidation(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	login := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + login.AccessToken}

	weak := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/me/password", `{"currentPassword":"long-enough-password","newPassword":"short"}`, headers)
	if weak.Code != http.StatusBadRequest {
		t.Fatalf("weak password status=%d body=%s", weak.Code, weak.Body.String())
	}

	changed := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/me/password", `{"currentPassword":"long-enough-password","newPassword":"replacement-password-value"}`, headers)
	if changed.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", changed.Code, changed.Body.String())
	}
	if changed.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("password change Cache-Control=%q", changed.Header().Get("Cache-Control"))
	}

	oldAccess := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", headers)
	if oldAccess.Code != http.StatusUnauthorized {
		t.Fatalf("old access survived password change: status=%d body=%s", oldAccess.Code, oldAccess.Body.String())
	}
	oldRefresh := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", `{"refreshToken":"`+login.RefreshToken+`"}`, nil)
	if oldRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("old refresh survived password change: status=%d body=%s", oldRefresh.Code, oldRefresh.Body.String())
	}

	relogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"admin","password":"replacement-password-value"}`, nil)
	if relogin.Code != http.StatusOK {
		t.Fatalf("login with replacement password status=%d body=%s", relogin.Code, relogin.Body.String())
	}
}
