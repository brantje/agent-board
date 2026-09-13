package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestAuthManagementHTTPAlternateAndErrorPaths(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, authStore, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	first := loginAuthHTTPUser(t, handler)
	second := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + second.AccessToken}

	// The in-memory store predates UUID-constrained session routes. Give its two
	// sessions routable IDs so this HTTP test reaches the ownership/revoke path.
	firstDigest := sha256.Sum256([]byte(first.RefreshToken))
	firstKey := authHTTPHashKey(firstDigest[:])
	firstSessionID := "00000000-0000-0000-0000-000000000111"
	secondSessionID := "00000000-0000-0000-0000-000000000112"
	foundFirst := false
	authStore.mu.Lock()
	for key, session := range authStore.sessions {
		if key == firstKey {
			session.ID = firstSessionID
			foundFirst = true
		} else {
			session.ID = secondSessionID
		}
		authStore.sessions[key] = session
	}
	authStore.mu.Unlock()
	if !foundFirst {
		t.Fatal("first login session not found")
	}

	for _, path := range []string{"/api/auth/users", "/api/auth/me/sessions", "/api/auth/settings"} {
		response := authHTTPRequest(t, handler, http.MethodGet, path, "", nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	invalidBearer := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/users", "", map[string]string{"Authorization": "Bearer not-a-valid-token"})
	if invalidBearer.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer status=%d body=%s", invalidBearer.Code, invalidBearer.Body.String())
	}
	invalidID := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users/not-a-uuid/setup-token", "", headers)
	if invalidID.Code != http.StatusBadRequest {
		t.Fatalf("invalid user id status=%d body=%s", invalidID.Code, invalidID.Body.String())
	}

	settings := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/settings", "", headers)
	if settings.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", settings.Code, settings.Body.String())
	}
	invalidSettings := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/settings", `{"accessTokenLifetimeSeconds":1,"refreshTokenLifetimeSeconds":2,"minimumPasswordLength":12}`, headers)
	if invalidSettings.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status=%d body=%s", invalidSettings.Code, invalidSettings.Body.String())
	}

	sessionsResponse := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me/sessions", "", headers)
	if sessionsResponse.Code != http.StatusOK {
		t.Fatalf("sessions status=%d body=%s", sessionsResponse.Code, sessionsResponse.Body.String())
	}
	var sessions []authSessionResponse
	if err := json.Unmarshal(sessionsResponse.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected two sessions, got %+v", sessions)
	}

	missing := authHTTPRequest(t, handler, http.MethodDelete, "/api/auth/me/sessions/00000000-0000-0000-0000-000000000999", "", headers)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing session status=%d body=%s", missing.Code, missing.Body.String())
	}
	revoked := authHTTPRequest(t, handler, http.MethodDelete, "/api/auth/me/sessions/"+firstSessionID, "", headers)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	badLogoutOthers := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/me/sessions/logout-others", `{"refreshToken":"not-a-real-refresh-token"}`, headers)
	if badLogoutOthers.Code != http.StatusUnauthorized {
		t.Fatalf("bad logout-others status=%d body=%s", badLogoutOthers.Code, badLogoutOthers.Body.String())
	}

	me := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + second.AccessToken})
	if me.Code != http.StatusOK {
		t.Fatalf("current access status=%d body=%s firstRefresh=%q", me.Code, me.Body.String(), first.RefreshToken)
	}
}

func TestAuthManagementHTTPValidationAndConflictBranches(t *testing.T) {
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
}
