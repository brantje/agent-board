package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *authHTTPStore) ListUsers(context.Context) ([]store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]store.User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	return users, nil
}

func (s *authHTTPStore) UpdateUserIdentity(_ context.Context, id, username, email, displayName string) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for otherID, existing := range s.users {
		if otherID != id && (existing.Username == username || existing.Email == username || existing.Username == email || existing.Email == email) {
			return store.User{}, store.ErrConflict
		}
	}
	user, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	user.Username = username
	user.Email = email
	user.DisplayName = displayName
	s.users[id] = user
	return user, nil
}

func (s *authHTTPStore) ListUserAuthSessions(_ context.Context, userID string, now time.Time) ([]store.AuthSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := make([]store.AuthSession, 0)
	for _, session := range s.sessions {
		if session.UserID == userID && session.RevokedAt == nil && session.ExpiresAt.After(now) {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (s *authHTTPStore) RevokeAuthSession(_ context.Context, userID, sessionID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if session.UserID == userID && session.ID == sessionID && session.RevokedAt == nil {
			session.RevokedAt = &now
			s.sessions[key] = session
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *authHTTPStore) RevokeOtherAuthSessions(_ context.Context, userID, keepSessionID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if session.UserID == userID && session.ID != keepSessionID && session.RevokedAt == nil {
			session.RevokedAt = &now
			s.sessions[key] = session
		}
	}
	return nil
}

func (s *authHTTPStore) UpdateAuthSettings(_ context.Context, settings store.AuthSettings) (store.AuthSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
	return settings, nil
}

func TestAuthPhase2AdminCreatesPendingUserAndSecretIsNoStore(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)

	response := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"member","email":"member@example.com","displayName":"Member"}`, map[string]string{"Authorization": "Bearer " + admin.AccessToken})
	if response.Code != http.StatusCreated {
		t.Fatalf("create pending status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create pending Cache-Control=%q", response.Header().Get("Cache-Control"))
	}
	var created pendingUserResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.User.Status != store.UserStatusPending || created.SetupToken == "" {
		t.Fatalf("unexpected pending user response: %+v", created)
	}

	list := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/users", "", map[string]string{"Authorization": "Bearer " + admin.AccessToken})
	if list.Code != http.StatusOK {
		t.Fatalf("list users status=%d body=%s", list.Code, list.Body.String())
	}
	if contains := string(list.Body.Bytes()); contains == "" || json.Valid(list.Body.Bytes()) == false {
		t.Fatalf("invalid user list response: %q", contains)
	}
	if string(list.Body.Bytes()) == response.Body.String() {
		t.Fatal("user list must not replay the one-time setup secret response")
	}
}

func TestAuthPhase2MemberCannotAdministerUsersOrSettings(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)

	createdResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"member","email":"member@example.com","displayName":"Member"}`, map[string]string{"Authorization": "Bearer " + admin.AccessToken})
	var created pendingUserResponse
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	setup := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", `{"token":"`+created.SetupToken+`","password":"member-long-password"}`, nil)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	memberLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"member-long-password"}`, nil)
	if memberLogin.Code != http.StatusOK {
		t.Fatalf("member login status=%d body=%s", memberLogin.Code, memberLogin.Body.String())
	}
	var member authTokensResponse
	if err := json.Unmarshal(memberLogin.Body.Bytes(), &member); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Authorization": "Bearer " + member.AccessToken}
	for _, path := range []string{"/api/auth/users", "/api/auth/settings"} {
		response := authHTTPRequest(t, handler, http.MethodGet, path, "", headers)
		if response.Code != http.StatusForbidden {
			t.Fatalf("member GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestAuthPhase2OwnProfileSessionsAndSettings(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	first := loginAuthHTTPUser(t, handler)
	second := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + second.AccessToken}

	profile := authHTTPRequest(t, handler, http.MethodPatch, "/api/auth/me", `{"username":"updated-admin","email":"updated@example.com","displayName":"Updated Admin"}`, headers)
	if profile.Code != http.StatusOK {
		t.Fatalf("profile status=%d body=%s", profile.Code, profile.Body.String())
	}

	sessions := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me/sessions", "", headers)
	if sessions.Code != http.StatusOK || sessions.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("sessions status=%d cache=%q body=%s", sessions.Code, sessions.Header().Get("Cache-Control"), sessions.Body.String())
	}
	var listed []authSessionResponse
	if err := json.Unmarshal(sessions.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected two own sessions, got %+v", listed)
	}

	logoutOthers := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/me/sessions/logout-others", `{"refreshToken":"`+second.RefreshToken+`"}`, headers)
	if logoutOthers.Code != http.StatusNoContent {
		t.Fatalf("logout others status=%d body=%s", logoutOthers.Code, logoutOthers.Body.String())
	}
	oldRefresh := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", `{"refreshToken":"`+first.RefreshToken+`"}`, nil)
	if oldRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("old session survived logout-others: status=%d body=%s", oldRefresh.Code, oldRefresh.Body.String())
	}
	currentRefresh := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", `{"refreshToken":"`+second.RefreshToken+`"}`, nil)
	if currentRefresh.Code != http.StatusOK {
		t.Fatalf("current session was revoked: status=%d body=%s", currentRefresh.Code, currentRefresh.Body.String())
	}

	settings := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/settings", `{"accessTokenLifetimeSeconds":7200,"refreshTokenLifetimeSeconds":604800,"minimumPasswordLength":14,"requireUppercase":true,"requireLowercase":true,"requireNumber":true,"requireSymbol":false}`, headers)
	if settings.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", settings.Code, settings.Body.String())
	}
	var stored authSettingsResponse
	if err := json.Unmarshal(settings.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.AccessTokenLifetimeSeconds != 7200 || stored.MinimumPasswordLength != 14 || !stored.RequireNumber {
		t.Fatalf("unexpected settings: %+v", stored)
	}
}
