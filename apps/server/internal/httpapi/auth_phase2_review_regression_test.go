package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type phase2AuthHTTPStore struct {
	*authHTTPStore
}

func (s *phase2AuthHTTPStore) SetUserPassword(_ context.Context, id, hash string, force bool) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	user.PasswordHash = hash
	if user.Status == store.UserStatusPending {
		user.Status = store.UserStatusActive
	}
	user.ForcePasswordChange = force
	user.AuthVersion++
	s.users[id] = user
	now := time.Unix(2, 0).UTC()
	s.revokeUserSessionsLocked(id, now)
	for key, token := range s.tokens {
		if token.UserID == id && token.ConsumedAt == nil && token.RevokedAt == nil {
			token.RevokedAt = &now
			s.tokens[key] = token
		}
	}
	return user, nil
}

func newPhase2AuthHTTPHandler(t *testing.T, now *time.Time) http.Handler {
	t.Helper()
	authStore := &phase2AuthHTTPStore{authHTTPStore: newAuthHTTPStore()}
	authService, err := app.NewAuthService(authStore, app.AuthServiceConfig{
		Now:        func() time.Time { return *now },
		Random:     &authHTTPRandom{},
		SigningKey: bytes.Repeat([]byte{17}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	services := &app.Services{
		ControlPlane: app.New(&fakeControlPlaneStore{}),
		Auth:         authService,
	}
	return NewRouterWithApplication(services)
}

func TestAuthPhase2HTTPPendingDirectPasswordActivatesAndRevokesSetup(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler := newPhase2AuthHTTPHandler(t, &now)
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

	setPassword := authHTTPRequest(t, handler, http.MethodPut, fmt.Sprintf("/api/auth/users/%s/password", created.User.ID), `{"password":"temporary-member-password"}`, adminHeaders)
	if setPassword.Code != http.StatusOK {
		t.Fatalf("pending direct password status=%d body=%s", setPassword.Code, setPassword.Body.String())
	}
	var assigned authUserResponse
	if err := json.Unmarshal(setPassword.Body.Bytes(), &assigned); err != nil {
		t.Fatal(err)
	}
	if assigned.Status != store.UserStatusActive || !assigned.ForcePasswordChange {
		t.Fatalf("pending direct password response = %+v", assigned)
	}

	oldSetup := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"replacement-member-password"}`, created.SetupToken), nil)
	if oldSetup.Code == http.StatusOK {
		t.Fatal("setup token remained usable after pending direct password assignment")
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
}

func TestAuthPhase2HTTPOneTimeTokenPlaintextOnlyAtResponseBoundary(t *testing.T) {
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
	if created.SetupToken == "" {
		t.Fatal("create response did not return one-time setup secret")
	}
	list := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/users", "", adminHeaders)
	if strings.Contains(list.Body.String(), created.SetupToken) {
		t.Fatal("user list exposed previously returned setup token plaintext")
	}

	replacementResponse := authHTTPRequest(t, handler, http.MethodPost, fmt.Sprintf("/api/auth/users/%s/setup-token", created.User.ID), "", adminHeaders)
	if replacementResponse.Code != http.StatusCreated {
		t.Fatalf("replacement setup status=%d body=%s", replacementResponse.Code, replacementResponse.Body.String())
	}
	var replacement passwordTokenResponse
	if err := json.Unmarshal(replacementResponse.Body.Bytes(), &replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.Token == "" || replacement.Token == created.SetupToken {
		t.Fatalf("unexpected replacement token: %+v", replacement)
	}
	oldSetup := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"member-long-password"}`, created.SetupToken), nil)
	if oldSetup.Code == http.StatusOK {
		t.Fatal("superseded setup token remained usable")
	}
	setup := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"member-long-password"}`, replacement.Token), nil)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	reuse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"another-long-password"}`, replacement.Token), nil)
	if reuse.Code == http.StatusOK {
		t.Fatal("consumed setup token remained reusable")
	}
	list = authHTTPRequest(t, handler, http.MethodGet, "/api/auth/users", "", adminHeaders)
	if strings.Contains(list.Body.String(), replacement.Token) {
		t.Fatal("user list exposed consumed setup token plaintext")
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
