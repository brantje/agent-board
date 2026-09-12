package app

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (m *authMemory) CreatePendingUserWithSetupToken(_ context.Context, user store.User, token store.PasswordToken) (store.User, store.PasswordToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	created, err := m.createUserLocked(user)
	if err != nil {
		return store.User{}, store.PasswordToken{}, err
	}
	token.UserID = created.ID
	if token.ID == "" {
		token.ID = m.id("token")
	}
	token.CreatedAt = time.Unix(1, 0).UTC()
	m.tokens[hashKey(token.TokenHash)] = token
	return created, token, nil
}

func (m *authMemory) ListUsers(context.Context) ([]store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]store.User, 0, len(m.users))
	for _, user := range m.users {
		result = append(result, user)
	}
	return result, nil
}

func (m *authMemory) UpdateUserIdentity(_ context.Context, id, username, email, displayName string) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for otherID, existing := range m.users {
		if otherID != id && (existing.Username == username || existing.Email == username || existing.Username == email || existing.Email == email) {
			return store.User{}, store.ErrConflict
		}
	}
	user, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	user.Username = username
	user.Email = email
	user.DisplayName = displayName
	m.users[id] = user
	return user, nil
}

func (m *authMemory) ListUserAuthSessions(_ context.Context, userID string, now time.Time) ([]store.AuthSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]store.AuthSession, 0)
	for _, session := range m.sessions {
		if session.UserID == userID && session.RevokedAt == nil && session.ExpiresAt.After(now) {
			result = append(result, session)
		}
	}
	return result, nil
}

func (m *authMemory) RevokeAuthSession(_ context.Context, userID, sessionID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, session := range m.sessions {
		if session.UserID == userID && session.ID == sessionID && session.RevokedAt == nil {
			session.RevokedAt = &now
			m.sessions[key] = session
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *authMemory) RevokeOtherAuthSessions(_ context.Context, userID, keepSessionID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, session := range m.sessions {
		if session.UserID == userID && session.ID != keepSessionID && session.RevokedAt == nil {
			session.RevokedAt = &now
			m.sessions[key] = session
		}
	}
	return nil
}

func (m *authMemory) UpdateAuthSettings(_ context.Context, settings store.AuthSettings) (store.AuthSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = settings
	return settings, nil
}

func phase2Admin() AuthenticatedUser {
	return AuthenticatedUser{ID: "admin", DeploymentRole: store.DeploymentRoleAdmin, Status: store.UserStatusActive}
}

func TestPhase2DeploymentMemberCannotAdministerUsersOrSettings(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	member := AuthenticatedUser{ID: "member", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	if _, err := service.ListUsers(context.Background(), member); err == nil {
		t.Fatal("expected member user administration to be forbidden")
	}
	if _, err := service.AuthSettings(context.Background(), member); err == nil {
		t.Fatal("expected member auth-settings access to be forbidden")
	}
}

func TestPhase2AdminCreatesPendingUserWithOneTimeSetupSecret(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	created, err := service.CreatePendingUser(context.Background(), phase2Admin(), PendingUserRegistration{
		Username: " New.User ", Email: " NEW@example.com ", DisplayName: " New User ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.User.Status != store.UserStatusPending || created.User.Username != "new.user" || created.User.Email != "new@example.com" {
		t.Fatalf("unexpected pending user: %+v", created.User)
	}
	if created.Setup.Token == "" || created.Setup.ExpiresAt.Sub(now) != passwordTokenTTL {
		t.Fatalf("unexpected setup secret: %+v", created.Setup)
	}
	listed, err := service.ListUsers(context.Background(), phase2Admin())
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
}

func TestPhase2ProfileNormalizationAndOwnSessionScope(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	memory.users["u1"] = store.User{ID: "u1", Username: "one", Email: "one@example.com", DisplayName: "One", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive, AuthVersion: 1}
	memory.sessions["one"] = store.AuthSession{ID: "s1", UserID: "u1", ExpiresAt: now.Add(time.Hour)}
	memory.sessions["other"] = store.AuthSession{ID: "s2", UserID: "u2", ExpiresAt: now.Add(time.Hour)}
	service := authTestService(t, memory, &now)
	actor := AuthenticatedUser{ID: "u1", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	updated, err := service.UpdateOwnProfile(context.Background(), actor, UserProfileUpdate{Username: " Updated ", Email: " UPDATED@example.com ", DisplayName: " Updated Name "})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Username != "updated" || updated.Email != "updated@example.com" || updated.DisplayName != "Updated Name" {
		t.Fatalf("unexpected profile: %+v", updated)
	}
	sessions, err := service.ListOwnSessions(context.Background(), actor)
	if err != nil || len(sessions) != 1 || sessions[0].ID != "s1" {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	if err := service.RevokeOwnSession(context.Background(), actor, "s1"); err != nil {
		t.Fatal(err)
	}
	sessions, _ = service.ListOwnSessions(context.Background(), actor)
	if len(sessions) != 0 {
		t.Fatalf("expected revoked session to disappear: %+v", sessions)
	}
}

func TestPhase2AuthSettingsValidationAndUpdate(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	bad := memory.settings
	bad.AccessTokenLifetime = time.Minute
	if _, err := service.UpdateAuthSettings(context.Background(), phase2Admin(), bad); err == nil {
		t.Fatal("expected access lifetime validation")
	}
	good := memory.settings
	good.PasswordPolicy.RequireNumber = true
	stored, err := service.UpdateAuthSettings(context.Background(), phase2Admin(), good)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.PasswordPolicy.RequireNumber {
		t.Fatalf("settings not updated: %+v", stored)
	}
}
