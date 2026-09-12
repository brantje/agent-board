package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type phase1OnlyAuthStore struct {
	store.AuthStore
}

type phase2MutationErrorStore struct {
	*phase2LifecycleMemory
	settingsErr error
	passwordErr error
	disabledErr error
}

func (m *phase2MutationErrorStore) GetAuthSettings(ctx context.Context) (store.AuthSettings, error) {
	if m.settingsErr != nil {
		return store.AuthSettings{}, m.settingsErr
	}
	return m.phase2LifecycleMemory.GetAuthSettings(ctx)
}

func (m *phase2MutationErrorStore) SetUserPasswordIfAuthVersion(ctx context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	if m.passwordErr != nil {
		return store.User{}, m.passwordErr
	}
	return m.phase2LifecycleMemory.SetUserPasswordIfAuthVersion(ctx, id, expectedAuthVersion, passwordHash, force)
}

func (m *phase2MutationErrorStore) SetUserDisabled(ctx context.Context, id string, disabled bool) (store.User, error) {
	if m.disabledErr != nil {
		return store.User{}, m.disabledErr
	}
	return m.phase2LifecycleMemory.SetUserDisabled(ctx, id, disabled)
}

func TestPhase2PasswordMutationErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	memory := &phase2LifecycleMemory{authMemory: newAuthMemory()}
	injected := errors.New("injected mutation failure")
	wrapped := &phase2MutationErrorStore{phase2LifecycleMemory: memory}
	service := phase2ReviewAuthTestService(t, wrapped, &now)

	wrapped.settingsErr = injected
	if _, err := service.setPasswordIfAuthVersion(ctx, "user", 1, "long-enough-password", false); !errors.Is(err, injected) {
		t.Fatalf("settings failure = %v, want injected failure", err)
	}
	wrapped.settingsErr = nil

	if _, err := service.setPasswordIfAuthVersion(ctx, "user", 1, "short", false); err == nil {
		t.Fatal("expected password policy failure")
	}

	phase1Service := phase2ReviewAuthTestService(t, phase1OnlyAuthStore{AuthStore: memory}, &now)
	if _, err := phase1Service.setPasswordIfAuthVersion(ctx, "user", 1, "long-enough-password", false); err == nil {
		t.Fatal("expected phase 2 store requirement failure")
	}

	wrapped.passwordErr = injected
	if _, err := service.setPasswordIfAuthVersion(ctx, "user", 1, "long-enough-password", false); !errors.Is(err, injected) {
		t.Fatalf("password mutation failure = %v, want injected failure", err)
	}
}

func TestPhase2PasswordChangeAuthenticationErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	memory := &phase2LifecycleMemory{authMemory: newAuthMemory()}
	service := phase2ReviewAuthTestService(t, memory, &now)

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

func TestPhase2AdminDisabledMutationErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	memory := &phase2LifecycleMemory{authMemory: newAuthMemory()}
	wrapped := &phase2MutationErrorStore{phase2LifecycleMemory: memory}
	service := phase2ReviewAuthTestService(t, wrapped, &now)
	admin := phase2Admin()

	wrapped.disabledErr = store.ErrConflict
	if _, err := service.AdminSetDisabled(ctx, admin, "user", true); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("last-admin conflict = %v, want conflict", err)
	}

	injected := errors.New("injected status failure")
	wrapped.disabledErr = injected
	if _, err := service.AdminSetDisabled(ctx, admin, "user", true); !errors.Is(err, injected) {
		t.Fatalf("status mutation failure = %v, want injected failure", err)
	}

	phase1Service := phase2ReviewAuthTestService(t, phase1OnlyAuthStore{AuthStore: memory}, &now)
	if _, err := phase1Service.AdminSetDisabled(ctx, admin, "user", true); err == nil {
		t.Fatal("expected phase 2 store requirement failure")
	}
}

func TestPhase2AdminPasswordAndStatusRules(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	memory.users["pending"] = store.User{
		ID: "pending", Username: "pending", Email: "pending@example.com", DisplayName: "Pending",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusPending, AuthVersion: 1,
	}
	memory.users["active"] = store.User{
		ID: "active", Username: "active", Email: "active@example.com", DisplayName: "Active",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive, PasswordHash: "existing-hash", AuthVersion: 1,
	}
	memory.users["disabled-no-password"] = store.User{
		ID: "disabled-no-password", Username: "disabled", Email: "disabled@example.com", DisplayName: "Disabled",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusDisabled, AuthVersion: 1,
	}
	service := authTestService(t, memory, &now)
	ctx := context.Background()
	admin := phase2Admin()

	if _, err := service.AdminCreatePasswordToken(ctx, admin, "pending", "invalid"); err == nil {
		t.Fatal("expected invalid token purpose to be rejected")
	}
	if _, err := service.AdminCreatePasswordToken(ctx, admin, "active", store.PasswordTokenPurposeSetup); err == nil {
		t.Fatal("expected setup token for active user to be rejected")
	}
	if _, err := service.AdminCreatePasswordToken(ctx, admin, "pending", store.PasswordTokenPurposeReset); err == nil {
		t.Fatal("expected reset token for pending user to be rejected")
	}
	setup, err := service.AdminCreatePasswordToken(ctx, admin, "pending", store.PasswordTokenPurposeSetup)
	if err != nil || setup.Token == "" {
		t.Fatalf("setup token = %+v, %v", setup, err)
	}
	reset, err := service.AdminCreatePasswordToken(ctx, admin, "active", store.PasswordTokenPurposeReset)
	if err != nil || reset.Token == "" {
		t.Fatalf("reset token = %+v, %v", reset, err)
	}

	disabledPending, err := service.AdminSetDisabled(ctx, admin, "pending", true)
	if err != nil || disabledPending.Status != store.UserStatusDisabled {
		t.Fatalf("disable pending = %+v, %v", disabledPending, err)
	}
	reenabledPending, err := service.AdminSetDisabled(ctx, admin, "pending", false)
	if err != nil || reenabledPending.Status != store.UserStatusPending {
		t.Fatalf("re-enable passwordless pending = %+v, %v", reenabledPending, err)
	}
	pendingChanged, err := service.AdminSetPassword(ctx, admin, "pending", "long-enough-password")
	if err != nil || !pendingChanged.ForcePasswordChange {
		t.Fatalf("pending direct password = %+v, %v", pendingChanged, err)
	}

	changed, err := service.AdminSetPassword(ctx, admin, "active", "new-long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	if !changed.ForcePasswordChange {
		t.Fatalf("direct password assignment must require a change: %+v", changed)
	}

	passwordless, err := service.AdminSetDisabled(ctx, admin, "disabled-no-password", false)
	if err != nil || passwordless.Status != store.UserStatusPending {
		t.Fatalf("passwordless user enable = %+v, %v", passwordless, err)
	}
	disabled, err := service.AdminSetDisabled(ctx, admin, "active", true)
	if err != nil || disabled.Status != store.UserStatusDisabled {
		t.Fatalf("disable = %+v, %v", disabled, err)
	}
	enabled, err := service.AdminSetDisabled(ctx, admin, "active", false)
	if err != nil || enabled.Status != store.UserStatusActive {
		t.Fatalf("enable = %+v, %v", enabled, err)
	}
}

func TestPhase2ProfileConflictSettingsAndAdminAuthentication(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	memory.users["admin"] = store.User{
		ID: "admin", Username: "admin", Email: "admin@example.com", DisplayName: "Admin",
		DeploymentRole: store.DeploymentRoleAdmin, Status: store.UserStatusActive, AuthVersion: 1,
	}
	memory.users["member"] = store.User{
		ID: "member", Username: "member", Email: "member@example.com", DisplayName: "Member",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive, AuthVersion: 1,
	}
	service := authTestService(t, memory, &now)
	ctx := context.Background()

	actor := AuthenticatedUser{ID: "member", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	if _, err := service.UpdateOwnProfile(ctx, actor, UserProfileUpdate{
		Username: "admin@example.com", Email: "new@example.com", DisplayName: "Collision",
	}); err == nil {
		t.Fatal("expected cross-user login namespace conflict")
	}

	settings, err := service.AuthSettings(ctx, phase2Admin())
	if err != nil {
		t.Fatal(err)
	}
	if settings.AccessTokenLifetime != time.Hour || settings.PasswordPolicy.MinimumLength != 12 {
		t.Fatalf("unexpected auth settings: %+v", settings)
	}
	inactiveAdmin := AuthenticatedUser{ID: "admin", DeploymentRole: store.DeploymentRoleAdmin, Status: store.UserStatusDisabled}
	if _, err := service.AuthSettings(ctx, inactiveAdmin); err == nil {
		t.Fatal("expected disabled admin to be rejected")
	}
}

func TestPhase2LogoutOtherSessionsValidatesCurrentSession(t *testing.T) {
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
