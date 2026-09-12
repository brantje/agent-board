package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestAuthPhase2HTTPAdminLifecycleAndPasswordInvalidation(t *testing.T) {
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

	replacementResponse := authHTTPRequest(t, handler, http.MethodPost, fmt.Sprintf("/api/auth/users/%s/setup-token", created.User.ID), "", adminHeaders)
	if replacementResponse.Code != http.StatusCreated || replacementResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("replace setup status=%d cache=%q body=%s", replacementResponse.Code, replacementResponse.Header().Get("Cache-Control"), replacementResponse.Body.String())
	}
	var replacement passwordTokenResponse
	if err := json.Unmarshal(replacementResponse.Body.Bytes(), &replacement); err != nil {
		t.Fatal(err)
	}
	oldSetup := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"member-long-password"}`, created.SetupToken), nil)
	if oldSetup.Code == http.StatusOK {
		t.Fatal("superseded setup token remained usable")
	}
	setup := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"member-long-password"}`, replacement.Token), nil)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}

	member := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"member-long-password"}`, nil)
	if member.Code != http.StatusOK {
		t.Fatalf("member login status=%d body=%s", member.Code, member.Body.String())
	}
	var memberTokens authTokensResponse
	if err := json.Unmarshal(member.Body.Bytes(), &memberTokens); err != nil {
		t.Fatal(err)
	}

	resetResponse := authHTTPRequest(t, handler, http.MethodPost, fmt.Sprintf("/api/auth/users/%s/reset-token", created.User.ID), "", adminHeaders)
	if resetResponse.Code != http.StatusCreated || resetResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("reset token status=%d cache=%q body=%s", resetResponse.Code, resetResponse.Header().Get("Cache-Control"), resetResponse.Body.String())
	}
	var reset passwordTokenResponse
	if err := json.Unmarshal(resetResponse.Body.Bytes(), &reset); err != nil {
		t.Fatal(err)
	}
	resetComplete := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/reset/complete", fmt.Sprintf(`{"token":%q,"password":"reset-long-password"}`, reset.Token), nil)
	if resetComplete.Code != http.StatusOK {
		t.Fatalf("reset completion status=%d body=%s", resetComplete.Code, resetComplete.Body.String())
	}
	reuse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/reset/complete", fmt.Sprintf(`{"token":%q,"password":"another-long-password"}`, reset.Token), nil)
	if reuse.Code == http.StatusOK {
		t.Fatal("reset token was reusable")
	}
	oldAccess := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + memberTokens.AccessToken})
	if oldAccess.Code != http.StatusUnauthorized {
		t.Fatalf("pre-reset access survived status=%d body=%s", oldAccess.Code, oldAccess.Body.String())
	}
	oldRefresh := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", fmt.Sprintf(`{"refreshToken":%q}`, memberTokens.RefreshToken), nil)
	if oldRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("pre-reset refresh survived status=%d body=%s", oldRefresh.Code, oldRefresh.Body.String())
	}

	postReset := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"reset-long-password"}`, nil)
	var postResetTokens authTokensResponse
	if err := json.Unmarshal(postReset.Body.Bytes(), &postResetTokens); err != nil {
		t.Fatal(err)
	}
	setPassword := authHTTPRequest(t, handler, http.MethodPut, fmt.Sprintf("/api/auth/users/%s/password", created.User.ID), `{"password":"admin-assigned-password"}`, adminHeaders)
	if setPassword.Code != http.StatusOK {
		t.Fatalf("admin set password status=%d body=%s", setPassword.Code, setPassword.Body.String())
	}
	var passwordUser authUserResponse
	if err := json.Unmarshal(setPassword.Body.Bytes(), &passwordUser); err != nil {
		t.Fatal(err)
	}
	if !passwordUser.ForcePasswordChange {
		t.Fatalf("direct password assignment did not force change: %+v", passwordUser)
	}
	if response := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + postResetTokens.AccessToken}); response.Code != http.StatusUnauthorized {
		t.Fatalf("pre-admin-password access survived status=%d body=%s", response.Code, response.Body.String())
	}

	forcedLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member@example.com","password":"admin-assigned-password"}`, nil)
	if forcedLogin.Code != http.StatusOK {
		t.Fatalf("forced login status=%d body=%s", forcedLogin.Code, forcedLogin.Body.String())
	}
	var forced authTokensResponse
	if err := json.Unmarshal(forcedLogin.Body.Bytes(), &forced); err != nil {
		t.Fatal(err)
	}
	if !forced.User.ForcePasswordChange {
		t.Fatalf("forced flag missing from login: %+v", forced.User)
	}
	disable := authHTTPRequest(t, handler, http.MethodPost, fmt.Sprintf("/api/auth/users/%s/disable", created.User.ID), "", adminHeaders)
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	if response := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + forced.AccessToken}); response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled access survived status=%d body=%s", response.Code, response.Body.String())
	}
	if response := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", fmt.Sprintf(`{"refreshToken":%q}`, forced.RefreshToken), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled refresh survived status=%d body=%s", response.Code, response.Body.String())
	}
	if response := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"admin-assigned-password"}`, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled login status=%d body=%s", response.Code, response.Body.String())
	}
	enable := authHTTPRequest(t, handler, http.MethodPost, fmt.Sprintf("/api/auth/users/%s/enable", created.User.ID), "", adminHeaders)
	if enable.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", enable.Code, enable.Body.String())
	}
	if response := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"admin-assigned-password"}`, nil); response.Code != http.StatusOK {
		t.Fatalf("re-enabled login status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthPhase2HTTPSelfPasswordSingleSessionRevokeAndSettingsDenial(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, authStore, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	adminHeaders := map[string]string{"Authorization": "Bearer " + admin.AccessToken}
	createdResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"member","email":"member@example.com","displayName":"Member"}`, adminHeaders)
	var created pendingUserResponse
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if response := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"member-long-password"}`, created.SetupToken), nil); response.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", response.Code, response.Body.String())
	}

	memberLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"member-long-password"}`, nil)
	var member authTokensResponse
	if err := json.Unmarshal(memberLogin.Body.Bytes(), &member); err != nil {
		t.Fatal(err)
	}
	memberHeaders := map[string]string{"Authorization": "Bearer " + member.AccessToken}
	settingsBody := `{"accessTokenLifetimeSeconds":7200,"refreshTokenLifetimeSeconds":604800,"minimumPasswordLength":14,"requireUppercase":true,"requireLowercase":true,"requireNumber":true,"requireSymbol":false}`
	if response := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/settings", settingsBody, memberHeaders); response.Code != http.StatusForbidden {
		t.Fatalf("member settings update status=%d body=%s", response.Code, response.Body.String())
	}

	change := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/me/password", `{"currentPassword":"member-long-password","newPassword":"self-service-password"}`, memberHeaders)
	if change.Code != http.StatusOK || change.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("self password status=%d cache=%q body=%s", change.Code, change.Header().Get("Cache-Control"), change.Body.String())
	}
	if response := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", memberHeaders); response.Code != http.StatusUnauthorized {
		t.Fatalf("old self-password access survived status=%d body=%s", response.Code, response.Body.String())
	}
	if response := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", fmt.Sprintf(`{"refreshToken":%q}`, member.RefreshToken), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("old self-password refresh survived status=%d body=%s", response.Code, response.Body.String())
	}

	firstResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"self-service-password"}`, nil)
	secondResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"self-service-password"}`, nil)
	var first, second authTokensResponse
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &first); err != nil { t.Fatal(err) }
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &second); err != nil { t.Fatal(err) }

	authStore.mu.Lock()
	n := 900
	for key, session := range authStore.sessions {
		if session.UserID == created.User.ID && session.RevokedAt == nil {
			n++
			session.ID = fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
			authStore.sessions[key] = session
		}
	}
	authStore.mu.Unlock()

	headers := map[string]string{"Authorization": "Bearer " + second.AccessToken}
	list := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me/sessions", "", headers)
	var sessions []authSessionResponse
	if err := json.Unmarshal(list.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected two active sessions, got %+v", sessions)
	}
	revoke := authHTTPRequest(t, handler, http.MethodDelete, "/api/auth/me/sessions/"+sessions[0].ID, "", headers)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("single-session revoke status=%d body=%s", revoke.Code, revoke.Body.String())
	}
	remaining := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me/sessions", "", headers)
	if err := json.Unmarshal(remaining.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("single-session revoke left %+v", sessions)
	}

	_ = first
}
