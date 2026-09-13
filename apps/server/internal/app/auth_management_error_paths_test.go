package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type authMutationErrorStore struct {
	*authLifecycleMemory
	settingsErr error
	passwordErr error
	disabledErr error
}

func (m *authMutationErrorStore) GetAuthSettings(ctx context.Context) (store.AuthSettings, error) {
	if m.settingsErr != nil {
		return store.AuthSettings{}, m.settingsErr
	}
	return m.authLifecycleMemory.GetAuthSettings(ctx)
}

func (m *authMutationErrorStore) SetUserPasswordIfAuthVersion(ctx context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	if m.passwordErr != nil {
		return store.User{}, m.passwordErr
	}
	return m.authLifecycleMemory.SetUserPasswordIfAuthVersion(ctx, id, expectedAuthVersion, passwordHash, force)
}

func (m *authMutationErrorStore) SetUserDisabled(ctx context.Context, id string, disabled bool) (store.User, error) {
	if m.disabledErr != nil {
		return store.User{}, m.disabledErr
	}
	return m.authLifecycleMemory.SetUserDisabled(ctx, id, disabled)
}

func TestPasswordMutationErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	memory := &authLifecycleMemory{authMemory: newAuthMemory()}
	injected := errors.New("injected mutation failure")
	wrapped := &authMutationErrorStore{authLifecycleMemory: memory}
	service := reviewAuthTestService(t, wrapped, &now)

	wrapped.settingsErr = injected
	if _, err := service.setPasswordIfAuthVersion(ctx, "user", 1, "long-enough-password", false); !errors.Is(err, injected) {
		t.Fatalf("settings failure = %v, want injected failure", err)
	}
	wrapped.settingsErr = nil

	if _, err := service.setPasswordIfAuthVersion(ctx, "user", 1, "short", false); err == nil {
		t.Fatal("expected password policy failure")
	}

	wrapped.passwordErr = injected
	if _, err := service.setPasswordIfAuthVersion(ctx, "user", 1, "long-enough-password", false); !errors.Is(err, injected) {
		t.Fatalf("password mutation failure = %v, want injected failure", err)
	}
}

func TestPasswordChangeAuthenticationErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	memory := &authLifecycleMemory{authMemory: newAuthMemory()}
	service := reviewAuthTestService(t, memory, &now)

	passwordHash, err := service.hashPassword("current-long-password")
	if err != nil {
		t.Fatal(err)
	}
	memory.users["member"] = store.User{
		ID: "member", Username: "member", Email: "member@example.com", DisplayName: "Member",
		PasswordHash: passwordHash, DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive, AuthVersion: 3,
	}
	actor := AuthenticatedUser{ID: "member", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}

	if _, err := service.ChangeOwnPassword(ctx, actor, "", "replacement-long-password"); err == nil {
		t.Fatal("expected missing current password to fail")
	}
	if _, err := service.ChangeOwnPassword(ctx, actor, "wrong-long-password", "replacement-long-password"); err == nil {
		t.Fatal("expected wrong current password to fail")
	}
	missing := actor
	missing.ID = "missing"
	if _, err := service.ChangeOwnPassword(ctx, missing, "current-long-password", "replacement-long-password"); err == nil {
		t.Fatal("expected missing user to fail")
	}
	forcedMissing := missing
	forcedMissing.ForcePasswordChange = true
	forcedMissing.AuthVersion = 0
	if _, err := service.ChangeOwnPassword(ctx, forcedMissing, "", "replacement-long-password"); err == nil {
		t.Fatal("expected forced-change fallback for missing user to fail")
	}
}

func TestAdminDisabledMutationErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	memory := &authLifecycleMemory{authMemory: newAuthMemory()}
	wrapped := &authMutationErrorStore{authLifecycleMemory: memory}
	service := reviewAuthTestService(t, wrapped, &now)
	admin := deploymentAdminActor()

	wrapped.disabledErr = store.ErrConflict
	if _, err := service.AdminSetDisabled(ctx, admin, "user", true); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("last-admin conflict = %v, want conflict", err)
	}

	injected := errors.New("injected status failure")
	wrapped.disabledErr = injected
	if _, err := service.AdminSetDisabled(ctx, admin, "user", true); !errors.Is(err, injected) {
		t.Fatalf("status mutation failure = %v, want injected failure", err)
	}
}

func TestLogoutOtherSessionsValidatesCurrentSession(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	memory.users["u1"] = store.User{
		ID: "u1", Username: "one", Email: "one@example.com", DisplayName: "One",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive, AuthVersion: 1,
	}
	service := authTestService(t, memory, &now)
	actor := AuthenticatedUser{ID: "u1", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	ctx := context.Background()

	currentToken, currentHash, err := service.newOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	current, err := memory.CreateAuthSession(ctx, store.AuthSession{
		ID: "current", UserID: actor.ID, RefreshTokenHash: currentHash, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, otherHash, err := service.newOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	other, err := memory.CreateAuthSession(ctx, store.AuthSession{
		ID: "other", UserID: actor.ID, RefreshTokenHash: otherHash, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.LogoutOtherSessions(ctx, actor, currentToken); err != nil {
		t.Fatal(err)
	}
	memory.mu.Lock()
	currentStored := memory.sessions[hashKey(currentHash)]
	otherStored := memory.sessions[hashKey(otherHash)]
	memory.mu.Unlock()
	if currentStored.ID != current.ID || currentStored.RevokedAt != nil {
		t.Fatalf("current session was revoked: %+v", currentStored)
	}
	if otherStored.ID != other.ID || otherStored.RevokedAt == nil {
		t.Fatalf("other session was not revoked: %+v", otherStored)
	}

	if err := service.LogoutOtherSessions(ctx, actor, "missing-refresh-token"); err == nil {
		t.Fatal("expected unknown current refresh token to fail")
	}
	otherActor := AuthenticatedUser{ID: "u2", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	if err := service.LogoutOtherSessions(ctx, otherActor, currentToken); err == nil {
		t.Fatal("expected current session owned by another user to fail")
	}

	memory.mu.Lock()
	revokedAt := now
	currentStored.RevokedAt = &revokedAt
	memory.sessions[hashKey(currentHash)] = currentStored
	memory.mu.Unlock()
	if err := service.LogoutOtherSessions(ctx, actor, currentToken); err == nil {
		t.Fatal("expected revoked current session to fail")
	}
}
