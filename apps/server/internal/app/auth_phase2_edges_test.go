package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestPhase2AdministrationRejectsInvalidUserStates(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	ctx := context.Background()
	admin := phase2Admin()

	memory.users["active"] = store.User{
		ID: "active", Username: "active", Email: "active@example.com", DisplayName: "Active",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive,
		PasswordHash: "existing-hash", AuthVersion: 1,
	}
	memory.users["pending"] = store.User{
		ID: "pending", Username: "pending", Email: "pending@example.com", DisplayName: "Pending",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusPending, AuthVersion: 1,
	}
	memory.users["disabled-no-password"] = store.User{
		ID: "disabled-no-password", Username: "disabled", Email: "disabled@example.com", DisplayName: "Disabled",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusDisabled, AuthVersion: 1,
	}

	if _, err := service.CreatePendingUser(ctx, admin, PendingUserRegistration{}); err == nil {
		t.Fatal("expected incomplete pending-user identity to be rejected")
	}
	if _, err := service.CreatePendingUser(ctx, admin, PendingUserRegistration{
		Username: "active", Email: "new@example.com", DisplayName: "Duplicate",
	}); err == nil {
		t.Fatal("expected duplicate pending-user identity to conflict")
	}

	if _, err := service.AdminCreatePasswordToken(ctx, admin, "active", "invalid-purpose"); err == nil {
		t.Fatal("expected invalid password-token purpose to be rejected")
	}
	if _, err := service.AdminCreatePasswordToken(ctx, admin, "active", store.PasswordTokenPurposeSetup); err == nil {
		t.Fatal("expected setup token for active user to be rejected")
	}
	if _, err := service.AdminCreatePasswordToken(ctx, admin, "pending", store.PasswordTokenPurposeReset); err == nil {
		t.Fatal("expected reset token for pending user to be rejected")
	}
	if _, err := service.AdminCreatePasswordToken(ctx, admin, "missing", store.PasswordTokenPurposeReset); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing user password-token error = %v", err)
	}

	if _, err := service.AdminSetPassword(ctx, admin, "pending", "long-enough-password"); err == nil {
		t.Fatal("expected direct password assignment for pending user to be rejected")
	}
	if _, err := service.AdminSetDisabled(ctx, admin, "pending", true); err == nil {
		t.Fatal("expected pending user disable to be rejected")
	}
	if _, err := service.AdminSetDisabled(ctx, admin, "disabled-no-password", false); err == nil {
		t.Fatal("expected passwordless disabled user enable to be rejected")
	}
}

func TestPhase2SelfServiceRejectsConflictsAndInvalidSessions(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	memory.users["u1"] = store.User{
		ID: "u1", Username: "one", Email: "one@example.com", DisplayName: "One",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive, AuthVersion: 1,
	}
	memory.users["u2"] = store.User{
		ID: "u2", Username: "two", Email: "two@example.com", DisplayName: "Two",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive, AuthVersion: 1,
	}
	service := authTestService(t, memory, &now)
	ctx := context.Background()
	actor := AuthenticatedUser{ID: "u1", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}

	if _, err := service.UpdateOwnProfile(ctx, actor, UserProfileUpdate{
		Username: "two", Email: "one-new@example.com", DisplayName: "One",
	}); err == nil {
		t.Fatal("expected conflicting profile identity to be rejected")
	}
	if err := service.RevokeOwnSession(ctx, actor, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing session revoke error = %v", err)
	}
	if err := service.LogoutOtherSessions(ctx, actor, ""); err == nil {
		t.Fatal("expected empty current refresh token to be rejected")
	}
}

func TestPhase2AuthSettingsRejectInvalidPersistedSettings(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	memory.settings.AccessTokenLifetime = time.Minute
	service := authTestService(t, memory, &now)

	if _, err := service.AuthSettings(context.Background(), phase2Admin()); err == nil {
		t.Fatal("expected invalid persisted auth settings to be rejected")
	}
}

func TestPhase2AuthenticateDeploymentAdmin(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	ctx := context.Background()

	admin, err := service.Bootstrap(ctx, BootstrapRegistration{
		Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: "long-enough-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := service.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := service.AuthenticateDeploymentAdmin(ctx, tokens.AccessToken)
	if err != nil || resolved.ID != admin.ID {
		t.Fatalf("resolved admin = %+v, err = %v", resolved, err)
	}
	if _, err := service.AuthenticateDeploymentAdmin(ctx, "not-an-access-token"); err == nil {
		t.Fatal("expected invalid access token to be rejected")
	}

	member := memory.users[admin.ID]
	member.DeploymentRole = store.DeploymentRoleMember
	memory.users[admin.ID] = member
	if _, err := service.AuthenticateDeploymentAdmin(ctx, tokens.AccessToken); err == nil {
		t.Fatal("expected valid non-admin access token to be forbidden")
	}
}
