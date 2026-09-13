package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type authLifecycleMemory struct {
	*authMemory
}

func (m *authLifecycleMemory) SetUserPassword(_ context.Context, id, passwordHash string, force bool) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	user.PasswordHash = passwordHash
	if user.Status == store.UserStatusPending {
		user.Status = store.UserStatusActive
	}
	user.ForcePasswordChange = force
	user.AuthVersion++
	m.users[id] = user
	now := time.Unix(2, 0).UTC()
	m.revokeUserSessionsLocked(id, now)
	for key, token := range m.tokens {
		if token.UserID == id && token.ConsumedAt == nil && token.RevokedAt == nil {
			token.RevokedAt = &now
			m.tokens[key] = token
		}
	}
	return user, nil
}

func reviewAuthTestService(t *testing.T, authStore store.AuthStore, now *time.Time) *AuthService {
	t.Helper()
	service, err := NewAuthService(authStore, AuthServiceConfig{
		Now:        func() time.Time { return *now },
		Random:     &deterministicReader{},
		SigningKey: bytes.Repeat([]byte{9}, 32),
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return service
}

func TestAdminDirectPasswordActivatesPendingAndInvalidatesSetup(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := &authLifecycleMemory{authMemory: newAuthMemory()}
	service := reviewAuthTestService(t, memory, &now)
	admin := bootstrapTestUser(t, service)

	pending, err := service.CreatePendingUser(ctx, admin, PendingUserRegistration{
		Username: "member", Email: "member@example.com", DisplayName: "Member",
	})
	if err != nil {
		t.Fatal(err)
	}
	setupHash, err := hashOpaqueToken(pending.Setup.Token)
	if err != nil {
		t.Fatal(err)
	}
	memory.mu.Lock()
	_, storedHashed := memory.tokens[hashKey(setupHash)]
	_, storedPlaintext := memory.tokens[pending.Setup.Token]
	memory.mu.Unlock()
	if !storedHashed || storedPlaintext {
		t.Fatalf("setup token storage hashed=%v plaintext=%v", storedHashed, storedPlaintext)
	}

	assigned, err := service.AdminSetPassword(ctx, admin, pending.User.ID, "temporary-member-password")
	if err != nil {
		t.Fatal(err)
	}
	if assigned.Status != store.UserStatusActive || !assigned.ForcePasswordChange {
		t.Fatalf("pending direct password assignment = %+v", assigned)
	}
	if _, err := service.CompletePasswordToken(ctx, pending.Setup.Token, store.PasswordTokenPurposeSetup, "replacement-member-password"); err == nil {
		t.Fatal("setup token remained usable after direct password assignment")
	}
	login, err := service.Login(ctx, "member", "temporary-member-password")
	if err != nil {
		t.Fatal(err)
	}
	if !login.User.ForcePasswordChange {
		t.Fatalf("temporary-password login did not require password change: %+v", login.User)
	}
}

func TestPendingDisableReenableReturnsToPendingWithoutPassword(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	admin := bootstrapTestUser(t, service)
	pending, err := service.CreatePendingUser(ctx, admin, PendingUserRegistration{
		Username: "member", Email: "member@example.com", DisplayName: "Member",
	})
	if err != nil {
		t.Fatal(err)
	}

	disabled, err := service.AdminSetDisabled(ctx, admin, pending.User.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != store.UserStatusDisabled {
		t.Fatalf("pending disable = %+v", disabled)
	}
	reenabled, err := service.AdminSetDisabled(ctx, admin, pending.User.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if reenabled.Status != store.UserStatusPending {
		t.Fatalf("passwordless re-enable status = %q want pending", reenabled.Status)
	}
}

func TestForcedPasswordChangeBlocksNormalApplicationAccess(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	admin := bootstrapTestUser(t, service)

	initial, err := service.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := service.AdminSetPassword(ctx, admin, admin.ID, "temporary-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if !assigned.ForcePasswordChange {
		t.Fatalf("admin direct assignment did not force change: %+v", assigned)
	}
	if _, err := service.AuthenticateAccessToken(ctx, initial.AccessToken); err == nil {
		t.Fatal("pre-assignment access token remained valid")
	}
	if _, err := service.Refresh(ctx, initial.RefreshToken); err == nil {
		t.Fatal("pre-assignment refresh token remained valid")
	}

	forced, err := service.Login(ctx, "admin", "temporary-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if !forced.User.ForcePasswordChange {
		t.Fatalf("forced state missing from login: %+v", forced.User)
	}
	if _, err := service.AuthenticateAccessToken(ctx, forced.AccessToken); err != nil {
		t.Fatalf("minimal self authentication should remain available: %v", err)
	}
	if _, err := service.AuthenticateNormalAccess(ctx, forced.AccessToken); err == nil {
		t.Fatal("forced-change user retained normal authenticated access")
	}
	if _, err := service.AuthenticateDeploymentAdmin(ctx, forced.AccessToken); err == nil {
		t.Fatal("forced-change admin retained deployment-admin access")
	}
	if _, err := service.ListOwnSessions(ctx, forced.User); err == nil {
		t.Fatal("forced-change user retained normal self-service session access")
	}

	changed, err := service.ChangeOwnPassword(ctx, forced.User, "", "permanent-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if changed.ForcePasswordChange {
		t.Fatalf("self password change did not clear forced state: %+v", changed)
	}
	if _, err := service.AuthenticateAccessToken(ctx, forced.AccessToken); err == nil {
		t.Fatal("forced-change access token survived password completion")
	}
	if _, err := service.Refresh(ctx, forced.RefreshToken); err == nil {
		t.Fatal("forced-change refresh token survived password completion")
	}

	normal, err := service.Login(ctx, "admin", "permanent-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if normal.User.ForcePasswordChange {
		t.Fatalf("re-login still forced: %+v", normal.User)
	}
	if _, err := service.AuthenticateDeploymentAdmin(ctx, normal.AccessToken); err != nil {
		t.Fatalf("normal deployment-admin access not restored: %v", err)
	}
}

func TestPersistedPasswordPolicyCoversEveryPasswordPath(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	memory.settings.PasswordPolicy = store.PasswordPolicy{
		MinimumLength:    14,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
		RequireSymbol:    true,
	}
	service := authTestService(t, memory, &now)

	weak := "weak-password"
	if _, err := service.Bootstrap(ctx, BootstrapRegistration{
		Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: weak,
	}); err == nil {
		t.Fatal("bootstrap ignored persisted password policy")
	}
	admin, err := service.Bootstrap(ctx, BootstrapRegistration{
		Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: "ValidBootstrap123!",
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.CreatePendingUser(ctx, admin, PendingUserRegistration{
		Username: "member", Email: "member@example.com", DisplayName: "Member",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePasswordToken(ctx, pending.Setup.Token, store.PasswordTokenPurposeSetup, weak); err == nil {
		t.Fatal("setup completion ignored persisted password policy")
	}
	member, err := service.CompletePasswordToken(ctx, pending.Setup.Token, store.PasswordTokenPurposeSetup, "ValidSetup123!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ChangeOwnPassword(ctx, member, "ValidSetup123!", weak); err == nil {
		t.Fatal("self-service password change ignored persisted password policy")
	}

	reset, err := service.AdminCreatePasswordToken(ctx, admin, member.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, weak); err == nil {
		t.Fatal("reset completion ignored persisted password policy")
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, "ValidReset123!"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdminSetPassword(ctx, admin, member.ID, weak); err == nil {
		t.Fatal("admin direct password assignment ignored persisted password policy")
	}
	if _, err := service.AdminSetPassword(ctx, admin, member.ID, "ValidDirect123!"); err != nil {
		t.Fatal(err)
	}
	forced, err := service.Login(ctx, "member", "ValidDirect123!")
	if err != nil {
		t.Fatal(err)
	}
	if !forced.User.ForcePasswordChange {
		t.Fatal("admin direct password did not enter forced-change flow")
	}
	if _, err := service.ChangeOwnPassword(ctx, forced.User, "", weak); err == nil {
		t.Fatal("forced password completion ignored persisted password policy")
	}
	if _, err := service.ChangeOwnPassword(ctx, forced.User, "", "ValidSelfChange123!"); err != nil {
		t.Fatal(err)
	}
}
