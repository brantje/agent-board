package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type authMemory struct {
	mu       sync.Mutex
	settings store.AuthSettings
	users    map[string]store.User
	sessions map[string]store.AuthSession
	tokens   map[string]store.PasswordToken
	nextID   int
}

func newAuthMemory() *authMemory {
	return &authMemory{
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

func (m *authMemory) id(prefix string) string {
	m.nextID++
	return fmt.Sprintf("%s-%d", prefix, m.nextID)
}

func (m *authMemory) UserCount(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *authMemory) BootstrapUser(_ context.Context, user store.User) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) != 0 {
		return store.User{}, store.ErrConflict
	}
	return m.createUserLocked(user)
}

func (m *authMemory) CreateUser(_ context.Context, user store.User) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createUserLocked(user)
}

func (m *authMemory) createUserLocked(user store.User) (store.User, error) {
	for _, existing := range m.users {
		if existing.Username == user.Username || existing.Email == user.Username || existing.Username == user.Email || existing.Email == user.Email {
			return store.User{}, store.ErrConflict
		}
	}
	if user.ID == "" {
		user.ID = m.id("user")
	}
	if user.AuthVersion < 1 {
		user.AuthVersion = 1
	}
	now := time.Unix(1, 0).UTC()
	user.CreatedAt = now
	user.UpdatedAt = now
	m.users[user.ID] = user
	return user, nil
}

func (m *authMemory) GetUser(_ context.Context, id string) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return user, nil
}

func (m *authMemory) GetUserByLogin(_ context.Context, login string) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, user := range m.users {
		if user.Username == login || user.Email == login {
			return user, nil
		}
	}
	return store.User{}, store.ErrNotFound
}

func (m *authMemory) SetUserStatus(_ context.Context, id, status string) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	if user.Status != status {
		user.Status = status
		user.AuthVersion++
		m.users[id] = user
		m.revokeUserSessionsLocked(id, time.Unix(2, 0).UTC())
	}
	return user, nil
}

func (m *authMemory) SetUserPassword(_ context.Context, id, passwordHash string, force bool) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	user.PasswordHash = passwordHash
	user.ForcePasswordChange = force
	user.AuthVersion++
	m.users[id] = user
	m.revokeUserSessionsLocked(id, time.Unix(2, 0).UTC())
	return user, nil
}

func (m *authMemory) GetAuthSettings(context.Context) (store.AuthSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings, nil
}

func hashKey(hash []byte) string { return fmt.Sprintf("%x", hash) }

func (m *authMemory) CreateAuthSession(_ context.Context, session store.AuthSession) (store.AuthSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session.ID == "" {
		session.ID = m.id("session")
	}
	session.CreatedAt = time.Unix(1, 0).UTC()
	m.sessions[hashKey(session.RefreshTokenHash)] = session
	return session, nil
}

func (m *authMemory) GetAuthSessionByRefreshHash(_ context.Context, hash []byte) (store.AuthSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[hashKey(hash)]
	if !ok {
		return store.AuthSession{}, store.ErrNotFound
	}
	return session, nil
}

func (m *authMemory) RotateAuthSession(_ context.Context, id string, oldHash, newHash []byte, expiresAt, now time.Time) (store.AuthSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldKey := hashKey(oldHash)
	session, ok := m.sessions[oldKey]
	if !ok || session.ID != id || session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return store.AuthSession{}, store.ErrNotFound
	}
	delete(m.sessions, oldKey)
	session.RefreshTokenHash = append([]byte(nil), newHash...)
	session.ExpiresAt = expiresAt
	session.LastUsedAt = &now
	m.sessions[hashKey(newHash)] = session
	return session, nil
}

func (m *authMemory) RevokeAuthSessionByRefreshHash(_ context.Context, hash []byte, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := hashKey(hash)
	session, ok := m.sessions[key]
	if !ok || session.RevokedAt != nil {
		return store.ErrNotFound
	}
	session.RevokedAt = &now
	m.sessions[key] = session
	return nil
}

func (m *authMemory) RevokeUserAuthSessions(_ context.Context, userID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revokeUserSessionsLocked(userID, now)
	return nil
}

func (m *authMemory) revokeUserSessionsLocked(userID string, now time.Time) {
	for key, session := range m.sessions {
		if session.UserID == userID && session.RevokedAt == nil {
			session.RevokedAt = &now
			m.sessions[key] = session
		}
	}
}

func (m *authMemory) CreatePasswordToken(_ context.Context, token store.PasswordToken) (store.PasswordToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Unix(1, 0).UTC()
	for key, existing := range m.tokens {
		if existing.UserID == token.UserID && existing.Purpose == token.Purpose && existing.ConsumedAt == nil && existing.RevokedAt == nil {
			existing.RevokedAt = &now
			m.tokens[key] = existing
		}
	}
	if token.ID == "" {
		token.ID = m.id("token")
	}
	token.CreatedAt = now
	m.tokens[hashKey(token.TokenHash)] = token
	return token, nil
}

func (m *authMemory) CompletePasswordToken(_ context.Context, hash []byte, purpose, passwordHash string, now time.Time) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := hashKey(hash)
	token, ok := m.tokens[key]
	if !ok || token.Purpose != purpose || token.ConsumedAt != nil || token.RevokedAt != nil || !token.ExpiresAt.After(now) {
		return store.User{}, store.ErrNotFound
	}
	user, ok := m.users[token.UserID]
	if !ok || (purpose == store.PasswordTokenPurposeSetup && user.Status != store.UserStatusPending) {
		return store.User{}, store.ErrNotFound
	}
	token.ConsumedAt = &now
	m.tokens[key] = token
	user.PasswordHash = passwordHash
	user.ForcePasswordChange = false
	user.AuthVersion++
	if purpose == store.PasswordTokenPurposeSetup {
		user.Status = store.UserStatusActive
	}
	m.users[user.ID] = user
	m.revokeUserSessionsLocked(user.ID, now)
	return user, nil
}

func authTestService(t *testing.T, memory *authMemory, now *time.Time) *AuthService {
	t.Helper()
	random := bytes.NewReader(bytes.Repeat([]byte{1, 2, 3, 4, 5, 6, 7, 8}, 1024))
	service, err := NewAuthService(memory, AuthServiceConfig{
		Now:        func() time.Time { return *now },
		Random:     random,
		SigningKey: bytes.Repeat([]byte{9}, 32),
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return service
}

func bootstrapTestUser(t *testing.T, service *AuthService) AuthenticatedUser {
	t.Helper()
	user, err := service.Bootstrap(context.Background(), BootstrapRegistration{
		Username:    " Admin ",
		Email:       " Admin@Example.com ",
		DisplayName: " Administrator ",
		Password:    "long-enough-password",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return user
}

func TestAuthBootstrapNormalizesAndCloses(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)

	available, err := service.BootstrapAvailable(context.Background())
	if err != nil || !available {
		t.Fatalf("bootstrap available = %v, %v", available, err)
	}
	user := bootstrapTestUser(t, service)
	if user.Username != "admin" || user.Email != "admin@example.com" || user.DeploymentRole != store.DeploymentRoleAdmin || user.Status != store.UserStatusActive {
		t.Fatalf("unexpected bootstrap user: %#v", user)
	}
	if _, err := service.Bootstrap(context.Background(), BootstrapRegistration{
		Username: "second", Email: "second@example.com", DisplayName: "Second", Password: "long-enough-password",
	}); err == nil {
		t.Fatal("second bootstrap unexpectedly succeeded")
	}
}

func TestAuthLoginByUsernameAndEmailAndPasswordHash(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	bootstrapTestUser(t, service)

	for _, login := range []string{" ADMIN ", "ADMIN@EXAMPLE.COM"} {
		tokens, err := service.Login(context.Background(), login, "long-enough-password")
		if err != nil {
			t.Fatalf("login %q: %v", login, err)
		}
		if tokens.AccessToken == "" || tokens.RefreshToken == "" {
			t.Fatalf("login %q returned empty tokens", login)
		}
	}
	stored, err := memory.GetUserByLogin(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if stored.PasswordHash == "long-enough-password" || stored.PasswordHash == "" {
		t.Fatalf("password was not securely represented at rest: %q", stored.PasswordHash)
	}
	ok, err := verifyPassword(stored.PasswordHash, "long-enough-password")
	if err != nil || !ok {
		t.Fatalf("password verification = %v, %v", ok, err)
	}
}

func TestAuthPendingAndDisabledCannotLogin(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	bootstrapTestUser(t, service)
	stored, _ := memory.GetUserByLogin(context.Background(), "admin")

	for _, status := range []string{store.UserStatusPending, store.UserStatusDisabled} {
		stored.Status = status
		memory.mu.Lock()
		memory.users[stored.ID] = stored
		memory.mu.Unlock()
		if _, err := service.Login(context.Background(), "admin", "long-enough-password"); err == nil {
			t.Fatalf("%s user logged in", status)
		}
	}
}

func TestPasswordPolicySharedValidator(t *testing.T) {
	policy := store.PasswordPolicy{
		MinimumLength: 12, RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSymbol: true,
	}
	if err := ValidatePassword(policy, "ValidPassword1!"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	for _, password := range []string{"Short1!", "lowercaseonly1!", "UPPERCASEONLY1!", "NoNumberHere!", "NoSymbolHere1"} {
		if err := ValidatePassword(policy, password); err == nil {
			t.Fatalf("password %q unexpectedly accepted", password)
		}
	}
}

func TestAccessTokenUsesAuthoritativeUserState(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	user := bootstrapTestUser(t, service)
	tokens, err := service.Login(context.Background(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := service.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err != nil || got.ID != user.ID {
		t.Fatalf("authenticate access token = %#v, %v", got, err)
	}
	if _, err := service.SetPassword(context.Background(), user.ID, "another-long-password", false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err == nil {
		t.Fatal("old access token survived auth-version change")
	}
}

func TestRefreshRotatesHashAndMultipleSessionsRemainIndependent(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	bootstrapTestUser(t, service)
	first, err := service.Login(context.Background(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(context.Background(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	oldHash := sha256.Sum256([]byte(first.RefreshToken))
	memory.mu.Lock()
	_, storedPlaintext := memory.sessions[first.RefreshToken]
	_, storedHash := memory.sessions[hashKey(oldHash[:])]
	memory.mu.Unlock()
	if storedPlaintext || !storedHash {
		t.Fatalf("refresh token storage plaintext=%v hashed=%v", storedPlaintext, storedHash)
	}

	rotated, err := service.Refresh(context.Background(), first.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.RefreshToken == first.RefreshToken || !rotated.RefreshTokenExpiresAt.Equal(first.RefreshTokenExpiresAt) {
		t.Fatalf("unexpected rotation: %#v", rotated)
	}
	if _, err := service.Refresh(context.Background(), first.RefreshToken); err == nil {
		t.Fatal("replayed refresh token succeeded")
	}
	if _, err := service.Refresh(context.Background(), second.RefreshToken); err != nil {
		t.Fatalf("independent session failed: %v", err)
	}
}

func TestLogoutAndPasswordMutationInvalidateRefresh(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	user := bootstrapTestUser(t, service)

	loggedOut, _ := service.Login(context.Background(), "admin", "long-enough-password")
	if err := service.Logout(context.Background(), loggedOut.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), loggedOut.RefreshToken); err == nil {
		t.Fatal("logged-out refresh token succeeded")
	}

	changed, _ := service.Login(context.Background(), "admin", "long-enough-password")
	if _, err := service.SetPassword(context.Background(), user.ID, "new-long-enough-password", false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), changed.RefreshToken); err == nil {
		t.Fatal("refresh survived password change")
	}
}

func TestDisabledUserCannotRefresh(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	user := bootstrapTestUser(t, service)
	tokens, _ := service.Login(context.Background(), "admin", "long-enough-password")
	if _, err := service.SetStatus(context.Background(), user.ID, store.UserStatusDisabled); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), tokens.RefreshToken); err == nil {
		t.Fatal("disabled user refreshed")
	}
}

func TestPasswordTokenIsOneTimeExpiresAndUsesSharedPolicy(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	bootstrapTestUser(t, service)
	pending, err := memory.CreateUser(context.Background(), store.User{
		Username: "pending", Email: "pending@example.com", DisplayName: "Pending", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusPending, AuthVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := service.CreatePasswordToken(context.Background(), pending.ID, store.PasswordTokenPurposeSetup)
	if err != nil {
		t.Fatal(err)
	}
	if !secret.ExpiresAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("setup expiry = %v", secret.ExpiresAt)
	}
	if _, err := service.CompletePasswordToken(context.Background(), secret.Token, store.PasswordTokenPurposeSetup, "short"); err == nil {
		t.Fatal("setup accepted password outside shared policy")
	}
	activated, err := service.CompletePasswordToken(context.Background(), secret.Token, store.PasswordTokenPurposeSetup, "pending-long-password")
	if err != nil || activated.Status != store.UserStatusActive {
		t.Fatalf("complete setup = %#v, %v", activated, err)
	}
	if _, err := service.CompletePasswordToken(context.Background(), secret.Token, store.PasswordTokenPurposeSetup, "pending-long-password"); err == nil {
		t.Fatal("setup token reused")
	}

	reset, err := service.CreatePasswordToken(context.Background(), pending.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(24*time.Hour + time.Second)
	if _, err := service.CompletePasswordToken(context.Background(), reset.Token, store.PasswordTokenPurposeReset, "another-long-password"); err == nil {
		t.Fatal("expired reset token succeeded")
	}
}

func TestAccessTokenExpiryAndSignatureAreEnforced(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	bootstrapTestUser(t, service)
	tokens, _ := service.Login(context.Background(), "admin", "long-enough-password")
	now = now.Add(time.Hour + time.Second)
	if _, err := service.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err == nil {
		t.Fatal("expired access token succeeded")
	}
	other, err := NewAuthService(memory, AuthServiceConfig{SigningKey: bytes.Repeat([]byte{7}, 32), Now: func() time.Time { return now.Add(-time.Hour - time.Second) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err == nil {
		t.Fatal("wrong-signature access token succeeded")
	}
}

func TestNewAuthServiceRejectsWeakSigningKey(t *testing.T) {
	_, err := NewAuthService(newAuthMemory(), AuthServiceConfig{SigningKey: []byte("too short")})
	if err == nil {
		t.Fatal("weak signing key accepted")
	}
}

func TestAuthErrorsDoNotContainSecrets(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	bootstrapTestUser(t, service)
	secret := "definitely-wrong-secret"
	_, err := service.Login(context.Background(), "admin", secret)
	if err == nil {
		t.Fatal("invalid password accepted")
	}
	if bytes.Contains([]byte(err.Error()), []byte(secret)) {
		t.Fatalf("authentication error leaked secret: %v", err)
	}
}
