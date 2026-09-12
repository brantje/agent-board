package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAuthPhase2ActivatedUserAdminLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	create := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"member","email":"member@example.com","displayName":"Member"}`, headers)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var pending pendingUserResponse
	if err := json.Unmarshal(create.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/auth/users/" + pending.User.ID + "/reset-token", ""},
		{http.MethodPut, "/api/auth/users/" + pending.User.ID + "/password", `{"password":"member-direct-password"}`},
		{http.MethodPost, "/api/auth/users/" + pending.User.ID + "/disable", ""},
	} {
		response := authHTTPRequest(t, handler, request.method, request.path, request.body, headers)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("pending %s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
	}

	setupResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users/"+pending.User.ID+"/setup-token", "", headers)
	if setupResponse.Code != http.StatusCreated || setupResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("setup token status=%d cache=%q body=%s", setupResponse.Code, setupResponse.Header().Get("Cache-Control"), setupResponse.Body.String())
	}
	var setup passwordTokenResponse
	if err := json.Unmarshal(setupResponse.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	complete := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", `{"token":"`+setup.Token+`","password":"member-initial-password"}`, nil)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete setup status=%d body=%s", complete.Code, complete.Body.String())
	}

	reset := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users/"+pending.User.ID+"/reset-token", "", headers)
	if reset.Code != http.StatusCreated || reset.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("reset token status=%d cache=%q body=%s", reset.Code, reset.Header().Get("Cache-Control"), reset.Body.String())
	}
	var resetSecret passwordTokenResponse
	if err := json.Unmarshal(reset.Body.Bytes(), &resetSecret); err != nil || resetSecret.Token == "" {
		t.Fatalf("reset secret=%+v err=%v", resetSecret, err)
	}

	setPassword := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/users/"+pending.User.ID+"/password", `{"password":"member-direct-password"}`, headers)
	if setPassword.Code != http.StatusOK {
		t.Fatalf("set password status=%d body=%s", setPassword.Code, setPassword.Body.String())
	}
	var changed authUserResponse
	if err := json.Unmarshal(setPassword.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if !changed.ForcePasswordChange || changed.Status != store.UserStatusActive {
		t.Fatalf("unexpected directly assigned user: %+v", changed)
	}

	disable := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users/"+pending.User.ID+"/disable", "", headers)
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	var disabled authUserResponse
	if err := json.Unmarshal(disable.Body.Bytes(), &disabled); err != nil || disabled.Status != store.UserStatusDisabled {
		t.Fatalf("disabled=%+v err=%v", disabled, err)
	}
	blockedLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"member-direct-password"}`, nil)
	if blockedLogin.Code != http.StatusUnauthorized {
		t.Fatalf("disabled login status=%d body=%s", blockedLogin.Code, blockedLogin.Body.String())
	}

	enable := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users/"+pending.User.ID+"/enable", "", headers)
	if enable.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", enable.Code, enable.Body.String())
	}
	var enabled authUserResponse
	if err := json.Unmarshal(enable.Body.Bytes(), &enabled); err != nil || enabled.Status != store.UserStatusActive {
		t.Fatalf("enabled=%+v err=%v", enabled, err)
	}
	reenabledLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"member-direct-password"}`, nil)
	if reenabledLogin.Code != http.StatusOK {
		t.Fatalf("re-enabled login status=%d body=%s", reenabledLogin.Code, reenabledLogin.Body.String())
	}
}

func TestAuthPhase2HTTPAlternateAndErrorPaths(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	first := loginAuthHTTPUser(t, handler)
	second := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + second.AccessToken}

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
	revoked := authHTTPRequest(t, handler, http.MethodDelete, "/api/auth/me/sessions/"+sessions[0].ID, "", headers)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	badLogoutOthers := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/me/sessions/logout-others", `{"refreshToken":"not-a-real-refresh-token"}`, headers)
	if badLogoutOthers.Code != http.StatusUnauthorized {
		t.Fatalf("bad logout-others status=%d body=%s", badLogoutOthers.Code, badLogoutOthers.Body.String())
	}

	// Ensure the second login itself remains usable even though another own session was revoked.
	me := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + second.AccessToken})
	if me.Code != http.StatusOK {
		t.Fatalf("current access status=%d body=%s firstRefresh=%q", me.Code, me.Body.String(), first.RefreshToken)
	}
}
