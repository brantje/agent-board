package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAuthPhase2HTTPPendingDirectPasswordAndDisableLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	adminHeaders := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	createdResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"member","email":"member@example.com","displayName":"Member"}`, adminHeaders)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create pending status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created pendingUserResponse
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	list := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/users", "", adminHeaders)
	if strings.Contains(list.Body.String(), created.SetupToken) {
		t.Fatal("user list exposed previously returned setup token plaintext")
	}

	setPassword := authHTTPRequest(t, handler, http.MethodPut, fmt.Sprintf("/api/auth/users/%s/password", created.User.ID), `{"password":"temporary-member-password"}`, adminHeaders)
	if setPassword.Code != http.StatusOK {
		t.Fatalf("pending direct password status=%d body=%s", setPassword.Code, setPassword.Body.String())
	}
	var assigned authUserResponse
	if err := json.Unmarshal(setPassword.Body.Bytes(), &assigned); err != nil {
		t.Fatal(err)
	}
	if assigned.Status != "active" || !assigned.ForcePasswordChange {
		t.Fatalf("pending direct password response = %+v", assigned)
	}
	oldSetup := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"replacement-member-password"}`, created.SetupToken), nil)
	if oldSetup.Code == http.StatusOK {
		t.Fatal("setup token remained usable after direct password assignment")
	}
	login := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"temporary-member-password"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("temporary password login status=%d body=%s", login.Code, login.Body.String())
	}
	var forced authTokensResponse
	if err := json.Unmarshal(login.Body.Bytes(), &forced); err != nil {
		t.Fatal(err)
	}
	if !forced.User.ForcePasswordChange {
		t.Fatalf("temporary password login did not require change: %+v", forced.User)
	}

	pendingResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"pending2","email":"pending2@example.com","displayName":"Pending Two"}`, adminHeaders)
	var pending pendingUserResponse
	if err := json.Unmarshal(pendingResponse.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	disable := authHTTPRequest(t, handler, http.MethodPost, fmt.Sprintf("/api/auth/users/%s/disable", pending.User.ID), "", adminHeaders)
	if disable.Code != http.StatusOK {
		t.Fatalf("pending disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	var disabled authUserResponse
	if err := json.Unmarshal(disable.Body.Bytes(), &disabled); err != nil {
		t.Fatal(err)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("pending disable response = %+v", disabled)
	}
	enable := authHTTPRequest(t, handler, http.MethodPost, fmt.Sprintf("/api/auth/users/%s/enable", pending.User.ID), "", adminHeaders)
	if enable.Code != http.StatusOK {
		t.Fatalf("pending re-enable status=%d body=%s", enable.Code, enable.Body.String())
	}
	var reenabled authUserResponse
	if err := json.Unmarshal(enable.Body.Bytes(), &reenabled); err != nil {
		t.Fatal(err)
	}
	if reenabled.Status != "pending" {
		t.Fatalf("passwordless re-enable status=%q want pending", reenabled.Status)
	}
}

func TestAuthPhase2HTTPForcedPasswordChangeBlocksNormalAndAdminAccess(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	adminHeaders := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	setPassword := authHTTPRequest(t, handler, http.MethodPut, fmt.Sprintf("/api/auth/users/%s/password", admin.User.ID), `{"password":"temporary-admin-password"}`, adminHeaders)
	if setPassword.Code != http.StatusOK {
		t.Fatalf("admin temporary password status=%d body=%s", setPassword.Code, setPassword.Body.String())
	}
	login := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"admin","password":"temporary-admin-password"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("forced admin login status=%d body=%s", login.Code, login.Body.String())
	}
	var forced authTokensResponse
	if err := json.Unmarshal(login.Body.Bytes(), &forced); err != nil {
		t.Fatal(err)
	}
	if !forced.User.ForcePasswordChange {
		t.Fatalf("forced flag missing after temporary password: %+v", forced.User)
	}
	forcedHeaders := map[string]string{"Authorization": "Bearer " + forced.AccessToken}

	if me := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", forcedHeaders); me.Code != http.StatusOK {
		t.Fatalf("minimal self state unavailable during forced change: status=%d body=%s", me.Code, me.Body.String())
	}
	for _, path := range []string{"/api/auth/me/sessions", "/api/auth/users", "/api/auth/settings"} {
		response := authHTTPRequest(t, handler, http.MethodGet, path, "", forcedHeaders)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "password_change_required") {
			t.Fatalf("forced-change access %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}

	change := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/me/password", `{"password":"permanent-admin-password"}`, forcedHeaders)
	if change.Code != http.StatusOK {
		t.Fatalf("forced self password change status=%d body=%s", change.Code, change.Body.String())
	}
	var changed authUserResponse
	if err := json.Unmarshal(change.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.ForcePasswordChange {
		t.Fatalf("forced flag survived self change: %+v", changed)
	}
	if oldAccess := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", forcedHeaders); oldAccess.Code != http.StatusUnauthorized {
		t.Fatalf("forced access token survived password change status=%d body=%s", oldAccess.Code, oldAccess.Body.String())
	}
	if oldRefresh := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", fmt.Sprintf(`{"refreshToken":%q}`, forced.RefreshToken), nil); oldRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("forced refresh token survived password change status=%d body=%s", oldRefresh.Code, oldRefresh.Body.String())
	}

	normalLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"admin","password":"permanent-admin-password"}`, nil)
	if normalLogin.Code != http.StatusOK {
		t.Fatalf("normal re-login status=%d body=%s", normalLogin.Code, normalLogin.Body.String())
	}
	var normal authTokensResponse
	if err := json.Unmarshal(normalLogin.Body.Bytes(), &normal); err != nil {
		t.Fatal(err)
	}
	normalHeaders := map[string]string{"Authorization": "Bearer " + normal.AccessToken}
	for _, path := range []string{"/api/auth/me/sessions", "/api/auth/users", "/api/auth/settings"} {
		response := authHTTPRequest(t, handler, http.MethodGet, path, "", normalHeaders)
		if response.Code != http.StatusOK {
			t.Fatalf("normal access %s not restored status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestAuthPhase2HTTPInvalidSettingsUsesApplicationValidation(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	response := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/settings", `{"accessTokenLifetimeSeconds":299,"refreshTokenLifetimeSeconds":2592000,"minimumPasswordLength":12,"requireUppercase":false,"requireLowercase":false,"requireNumber":false,"requireSymbol":false}`, headers)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"auth_settings_invalid"`) || !strings.Contains(response.Body.String(), "access token lifetime is invalid") {
		t.Fatalf("invalid settings did not use application validation error: %s", response.Body.String())
	}
}
