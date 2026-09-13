package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type authHTTPStore struct {
	mu       sync.Mutex
	settings store.AuthSettings
	users    map[string]store.User
	sessions map[string]store.AuthSession
	tokens   map[string]store.PasswordToken
	nextUser int
	nextID   int
}

func newAuthHTTPStore() *authHTTPStore {
	return &authHTTPStore{
		settings: store.AuthSettings{
			AccessTokenLifetime:  time.Hour,
			RefreshTokenLifetime: 30 * 24 * time.Hour,
			PasswordPolicy:       store.PasswordPolicy{MinimumLength: 12},
		},
		users:    map[string]store.User{},
		sessions: map[string]store.AuthSession{},
		tokens:   map[string]store.PasswordToken{},
	}
}

func (s *authHTTPStore) UserCount(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.users), nil
}

func (s *authHTTPStore) BootstrapUser(_ context.Context, user store.User) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.users) != 0 {
		return store.User{}, store.ErrConflict
	}
	return s.createUserLocked(user)
}

func (s *authHTTPStore) CreateUser(_ context.Context, user store.User) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createUserLocked(user)
}

func (s *authHTTPStore) createUserLocked(user store.User) (store.User, error) {
	for _, existing := range s.users {
		if existing.Username == user.Username || existing.Email == user.Email || existing.Username == user.Email || existing.Email == user.Username {
			return store.User{}, store.ErrConflict
		}
	}
	s.nextUser++
	user.ID = fmt.Sprintf("00000000-0000-0000-0000-%012d", s.nextUser)
	if user.AuthVersion < 1 {
		user.AuthVersion = 1
	}
	user.CreatedAt = time.Unix(1, 0).UTC()
	user.UpdatedAt = user.CreatedAt
	s.users[user.ID] = user
	return user, nil
}

func (s *authHTTPStore) GetUser(_ context.Context, id string) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return user, nil
}

func (s *authHTTPStore) GetUserByLogin(_ context.Context, login string) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.users {
		if user.Username == login || user.Email == login {
			return user, nil
		}
	}
	return store.User{}, store.ErrNotFound
}

func (s *authHTTPStore) SetUserStatus(_ context.Context, id, status string) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	if user.Status != status {
		user.Status = status
		user.AuthVersion++
		s.revokeUserSessionsLocked(id, time.Unix(2, 0).UTC())
	}
	s.users[id] = user
	return user, nil
}

func (s *authHTTPStore) SetUserPassword(_ context.Context, id, hash string, force bool) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	user.PasswordHash = hash
	user.ForcePasswordChange = force
	user.AuthVersion++
	s.users[id] = user
	s.revokeUserSessionsLocked(id, time.Unix(2, 0).UTC())
	return user, nil
}

func (s *authHTTPStore) GetAuthSettings(context.Context) (store.AuthSettings, error) {
	return s.settings, nil
}

func authHTTPHashKey(hash []byte) string { return fmt.Sprintf("%x", hash) }

func (s *authHTTPStore) CreateAuthSession(_ context.Context, session store.AuthSession) (store.AuthSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	session.ID = fmt.Sprintf("session-%d", s.nextID)
	session.CreatedAt = time.Unix(1, 0).UTC()
	s.sessions[authHTTPHashKey(session.RefreshTokenHash)] = session
	return session, nil
}

func (s *authHTTPStore) GetAuthSessionByRefreshHash(_ context.Context, hash []byte) (store.AuthSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[authHTTPHashKey(hash)]
	if !ok {
		return store.AuthSession{}, store.ErrNotFound
	}
	return session, nil
}

func (s *authHTTPStore) RotateAuthSession(_ context.Context, id string, oldHash, newHash []byte, expiresAt, now time.Time) (store.AuthSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldKey := authHTTPHashKey(oldHash)
	session, ok := s.sessions[oldKey]
	if !ok || session.ID != id || session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return store.AuthSession{}, store.ErrNotFound
	}
	delete(s.sessions, oldKey)
	session.RefreshTokenHash = append([]byte(nil), newHash...)
	session.ExpiresAt = expiresAt
	session.LastUsedAt = &now
	s.sessions[authHTTPHashKey(newHash)] = session
	return session, nil
}

func (s *authHTTPStore) RevokeAuthSessionByRefreshHash(_ context.Context, hash []byte, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := authHTTPHashKey(hash)
	session, ok := s.sessions[key]
	if !ok || session.RevokedAt != nil {
		return store.ErrNotFound
	}
	session.RevokedAt = &now
	s.sessions[key] = session
	return nil
}

func (s *authHTTPStore) RevokeUserAuthSessions(_ context.Context, userID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokeUserSessionsLocked(userID, now)
	return nil
}

func (s *authHTTPStore) revokeUserSessionsLocked(userID string, now time.Time) {
	for key, session := range s.sessions {
		if session.UserID == userID && session.RevokedAt == nil {
			session.RevokedAt = &now
			s.sessions[key] = session
		}
	}
}

func (s *authHTTPStore) CreatePasswordToken(_ context.Context, token store.PasswordToken) (store.PasswordToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Unix(1, 0).UTC()
	for key, existing := range s.tokens {
		if existing.UserID == token.UserID && existing.Purpose == token.Purpose && existing.ConsumedAt == nil && existing.RevokedAt == nil {
			existing.RevokedAt = &now
			s.tokens[key] = existing
		}
	}
	s.nextID++
	token.ID = fmt.Sprintf("token-%d", s.nextID)
	token.CreatedAt = now
	s.tokens[authHTTPHashKey(token.TokenHash)] = token
	return token, nil
}

func (s *authHTTPStore) CompletePasswordToken(_ context.Context, hash []byte, purpose, passwordHash string, now time.Time) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := authHTTPHashKey(hash)
	token, ok := s.tokens[key]
	if !ok || token.Purpose != purpose || token.ConsumedAt != nil || token.RevokedAt != nil || !token.ExpiresAt.After(now) {
		return store.User{}, store.ErrNotFound
	}
	user, ok := s.users[token.UserID]
	if !ok || (purpose == store.PasswordTokenPurposeSetup && user.Status != store.UserStatusPending) {
		return store.User{}, store.ErrNotFound
	}
	token.ConsumedAt = &now
	s.tokens[key] = token
	user.PasswordHash = passwordHash
	user.ForcePasswordChange = false
	user.AuthVersion++
	if purpose == store.PasswordTokenPurposeSetup {
		user.Status = store.UserStatusActive
	}
	s.users[user.ID] = user
	s.revokeUserSessionsLocked(user.ID, now)
	return user, nil
}

type authHTTPRandom struct{ offset byte }

func (r *authHTTPRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.offset + byte(i)
	}
	r.offset += byte(len(p)) + 1
	return len(p), nil
}

func newAuthHTTPHandler(t *testing.T, now *time.Time) (http.Handler, *authHTTPStore, *app.AuthService) {
	t.Helper()
	authStore := newAuthHTTPStore()
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
	return NewRouterWithApplication(services), authStore, authService
}

func authHTTPRequest(t *testing.T, handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func registerAuthHTTPUser(t *testing.T, handler http.Handler) authUserResponse {
	t.Helper()
	response := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/bootstrap/register", `{"username":" Admin ","email":" Admin@Example.com ","displayName":" Administrator ","password":"long-enough-password"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("bootstrap register status=%d body=%s", response.Code, response.Body.String())
	}
	var user authUserResponse
	if err := json.Unmarshal(response.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	return user
}

func loginAuthHTTPUser(t *testing.T, handler http.Handler) authTokensResponse {
	t.Helper()
	response := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"ADMIN@EXAMPLE.COM","password":"long-enough-password"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("login Cache-Control=%q", response.Header().Get("Cache-Control"))
	}
	var tokens authTokensResponse
	if err := json.Unmarshal(response.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	return tokens
}

func TestAuthHTTPBootstrapLoginMeAndHeaderSpoofing(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)

	status := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/bootstrap", "", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"available":true`) {
		t.Fatalf("bootstrap status=%d body=%s", status.Code, status.Body.String())
	}
	user := registerAuthHTTPUser(t, handler)
	if user.Username != "admin" || user.Email != "admin@example.com" || user.DeploymentRole != store.DeploymentRoleAdmin || user.Status != store.UserStatusActive {
		t.Fatalf("unexpected registered user: %#v", user)
	}
	status = authHTTPRequest(t, handler, http.MethodGet, "/api/auth/bootstrap", "", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"available":false`) {
		t.Fatalf("closed bootstrap status=%d body=%s", status.Code, status.Body.String())
	}

	tokens := loginAuthHTTPUser(t, handler)
	spoofed := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{
		"X-User-ID":         user.ID,
		"X-Deployment-Role": store.DeploymentRoleAdmin,
	})
	if spoofed.Code != http.StatusUnauthorized {
		t.Fatalf("spoofed identity status=%d body=%s", spoofed.Code, spoofed.Body.String())
	}
	authorized := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + tokens.AccessToken})
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), `"username":"admin"`) {
		t.Fatalf("me status=%d body=%s", authorized.Code, authorized.Body.String())
	}
}

func TestAuthHTTPRefreshLogoutAndTokenResponses(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	initial := loginAuthHTTPUser(t, handler)

	refresh := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", fmt.Sprintf(`{"refreshToken":%q}`, initial.RefreshToken), nil)
	if refresh.Code != http.StatusOK || refresh.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("refresh status=%d cache=%q body=%s", refresh.Code, refresh.Header().Get("Cache-Control"), refresh.Body.String())
	}
	var rotated authTokensResponse
	if err := json.Unmarshal(refresh.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.RefreshToken == initial.RefreshToken || !rotated.RefreshTokenExpiresAt.Equal(initial.RefreshTokenExpiresAt) {
		t.Fatalf("unexpected refresh rotation: %#v", rotated)
	}
	replay := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", fmt.Sprintf(`{"refreshToken":%q}`, initial.RefreshToken), nil)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed refresh status=%d body=%s", replay.Code, replay.Body.String())
	}
	logout := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/logout", fmt.Sprintf(`{"refreshToken":%q}`, rotated.RefreshToken), nil)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	afterLogout := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", fmt.Sprintf(`{"refreshToken":%q}`, rotated.RefreshToken), nil)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out refresh status=%d body=%s", afterLogout.Code, afterLogout.Body.String())
	}
}

func TestAuthHTTPSetupResetAndStrictRequestContracts(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, authStore, authService := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)

	pending, err := authStore.CreateUser(context.Background(), store.User{
		Username: "pending", Email: "pending@example.com", DisplayName: "Pending", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusPending, AuthVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := authService.CreatePasswordToken(context.Background(), pending.ID, store.PasswordTokenPurposeSetup)
	if err != nil {
		t.Fatal(err)
	}
	setupResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", fmt.Sprintf(`{"token":%q,"password":"pending-long-password"}`, setup.Token), nil)
	if setupResponse.Code != http.StatusOK || strings.Contains(setupResponse.Body.String(), setup.Token) || strings.Contains(setupResponse.Body.String(), "pending-long-password") {
		t.Fatalf("setup status=%d body=%s", setupResponse.Code, setupResponse.Body.String())
	}
	reset, err := authService.CreatePasswordToken(context.Background(), pending.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatal(err)
	}
	resetResponse := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/reset/complete", fmt.Sprintf(`{"token":%q,"password":"reset-long-password"}`, reset.Token), nil)
	if resetResponse.Code != http.StatusOK || strings.Contains(resetResponse.Body.String(), reset.Token) || strings.Contains(resetResponse.Body.String(), "reset-long-password") {
		t.Fatalf("reset status=%d body=%s", resetResponse.Code, resetResponse.Body.String())
	}

	unknown := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"admin","password":"reset-long-password","admin":true}`, nil)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown auth field status=%d body=%s", unknown.Code, unknown.Body.String())
	}
}
