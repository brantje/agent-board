package app

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestUserLifecycleInvalidatesAuthenticationAndForcesAdminPasswordChange(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	admin := bootstrapTestUser(t, service)

	adminTokens, err := service.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	if authenticated, err := service.AuthenticateDeploymentAdmin(ctx, adminTokens.AccessToken); err != nil || authenticated.ID != admin.ID {
		t.Fatalf("deployment admin authentication = %#v, %v", authenticated, err)
	}

	pending, err := service.CreatePendingUser(ctx, admin, PendingUserRegistration{
		Username: " Member ", Email: " MEMBER@example.com ", DisplayName: " Member User ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pending.User.Status != store.UserStatusPending || pending.Setup.Token == "" || pending.Setup.ExpiresAt.Sub(now) != passwordTokenTTL {
		t.Fatalf("unexpected pending user: %+v", pending)
	}
	if _, err := service.AdminCreatePasswordToken(ctx, admin, pending.User.ID, store.PasswordTokenPurposeReset); err == nil {
		t.Fatal("pending user received reset token before setup")
	}
	disabledPending, err := service.AdminSetDisabled(ctx, admin, pending.User.ID, true)
	if err != nil || disabledPending.Status != store.UserStatusDisabled {
		t.Fatalf("disable pending user = %+v, %v", disabledPending, err)
	}
	reenabledPending, err := service.AdminSetDisabled(ctx, admin, pending.User.ID, false)
	if err != nil || reenabledPending.Status != store.UserStatusPending {
		t.Fatalf("re-enable pending user = %+v, %v", reenabledPending, err)
	}

	replacement, err := service.AdminCreatePasswordToken(ctx, admin, pending.User.ID, store.PasswordTokenPurposeSetup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePasswordToken(ctx, pending.Setup.Token, store.PasswordTokenPurposeSetup, "member-long-password"); err == nil {
		t.Fatal("superseded setup token completed")
	}
	member, err := service.CompletePasswordToken(ctx, replacement.Token, store.PasswordTokenPurposeSetup, "member-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if member.Status != store.UserStatusActive {
		t.Fatalf("setup did not activate member: %+v", member)
	}
	if _, err := service.AdminCreatePasswordToken(ctx, admin, member.ID, store.PasswordTokenPurposeSetup); err == nil {
		t.Fatal("active user received a setup token")
	}

	memberTokens, err := service.Login(ctx, "member", "member-long-password")
	if err != nil {
		t.Fatal(err)
	}
	reset, err := service.AdminCreatePasswordToken(ctx, admin, member.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatal(err)
	}
	if reset.Token == "" || reset.ExpiresAt.Sub(now) != passwordTokenTTL {
		t.Fatalf("unexpected reset secret: %+v", reset)
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, "reset-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, "another-long-password"); err == nil {
		t.Fatal("reset token was reusable")
	}
	if _, err := service.AuthenticateAccessToken(ctx, memberTokens.AccessToken); err == nil {
		t.Fatal("pre-reset access token remained valid")
	}
	if _, err := service.Refresh(ctx, memberTokens.RefreshToken); err == nil {
		t.Fatal("pre-reset refresh token remained valid")
	}

	postReset, err := service.Login(ctx, "member", "reset-long-password")
	if err != nil {
		t.Fatal(err)
	}
	direct, err := service.AdminSetPassword(ctx, admin, member.ID, "admin-assigned-password")
	if err != nil {
		t.Fatal(err)
	}
	if !direct.ForcePasswordChange {
		t.Fatalf("admin password assignment did not force next-login change: %+v", direct)
	}
	if _, err := service.AuthenticateAccessToken(ctx, postReset.AccessToken); err == nil {
		t.Fatal("pre-admin-password access token remained valid")
	}
	if _, err := service.Refresh(ctx, postReset.RefreshToken); err == nil {
		t.Fatal("pre-admin-password refresh token remained valid")
	}

	forced, err := service.Login(ctx, "member@example.com", "admin-assigned-password")
	if err != nil {
		t.Fatal(err)
	}
	if !forced.User.ForcePasswordChange {
		t.Fatalf("login did not expose forced-password state: %+v", forced.User)
	}
	if _, err := service.AdminSetDisabled(ctx, admin, member.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(ctx, "member", "admin-assigned-password"); err == nil {
		t.Fatal("disabled member logged in")
	}
	if _, err := service.AuthenticateAccessToken(ctx, forced.AccessToken); err == nil {
		t.Fatal("disabled member access token remained valid")
	}
	if _, err := service.Refresh(ctx, forced.RefreshToken); err == nil {
		t.Fatal("disabled member refresh token remained valid")
	}
	if enabled, err := service.AdminSetDisabled(ctx, admin, member.ID, false); err != nil || enabled.Status != store.UserStatusActive {
		t.Fatalf("re-enable = %+v, %v", enabled, err)
	}
	if _, err := service.Login(ctx, "member", "admin-assigned-password"); err != nil {
		t.Fatalf("re-enabled member could not authenticate: %v", err)
	}
}

func TestSharedProfilePasswordSettingsAndSessionPolicy(t *testing.T) {
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
	member, err := service.CompletePasswordToken(ctx, pending.Setup.Token, store.PasswordTokenPurposeSetup, "member-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateOwnProfile(ctx, admin, UserProfileUpdate{
		Username: " MEMBER ", Email: "other@example.com", DisplayName: "Conflict",
	}); err == nil {
		t.Fatal("profile update accepted another user's login identifier")
	}
	updated, err := service.UpdateOwnProfile(ctx, admin, UserProfileUpdate{
		Username: " ADMIN.UPDATED ", Email: " ADMIN.UPDATED@example.com ", DisplayName: " Updated Admin ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Username != "admin.updated" || updated.Email != "admin.updated@example.com" || updated.DisplayName != "Updated Admin" {
		t.Fatalf("profile normalization failed: %+v", updated)
	}

	settings := memory.settings
	settings.AccessTokenLifetime = 2 * time.Hour
	settings.RefreshTokenLifetime = 7 * 24 * time.Hour
	settings.PasswordPolicy = store.PasswordPolicy{
		MinimumLength:    14,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
		RequireSymbol:    true,
	}
	stored, err := service.UpdateAuthSettings(ctx, updated, settings)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessTokenLifetime != 2*time.Hour || stored.RefreshTokenLifetime != 7*24*time.Hour {
		t.Fatalf("settings not persisted: %+v", stored)
	}
	if _, err := service.AdminSetPassword(ctx, updated, member.ID, "not-complex-enough"); err == nil {
		t.Fatal("admin password path ignored shared password policy")
	}
	if _, err := service.ChangeOwnPassword(ctx, updated, "long-enough-password", "not-complex-enough"); err == nil {
		t.Fatal("self password path ignored shared password policy")
	}

	beforeChange, err := service.Login(ctx, "admin.updated", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	if beforeChange.AccessTokenExpiresAt.Sub(now) != 2*time.Hour || beforeChange.RefreshTokenExpiresAt.Sub(now) != 7*24*time.Hour {
		t.Fatalf("configured token lifetimes not used: access=%v refresh=%v", beforeChange.AccessTokenExpiresAt.Sub(now), beforeChange.RefreshTokenExpiresAt.Sub(now))
	}
	if _, err := service.ChangeOwnPassword(ctx, updated, "long-enough-password", "ValidPassword123!"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateAccessToken(ctx, beforeChange.AccessToken); err == nil {
		t.Fatal("self password change did not invalidate access")
	}
	if _, err := service.Refresh(ctx, beforeChange.RefreshToken); err == nil {
		t.Fatal("self password change did not invalidate refresh")
	}

	first, err := service.Login(ctx, "admin.updated", "ValidPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(ctx, "admin.updated@example.com", "ValidPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.LogoutOtherSessions(ctx, updated, second.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(ctx, first.RefreshToken); err == nil {
		t.Fatal("logout-others preserved an older session")
	}
	if _, err := service.Refresh(ctx, second.RefreshToken); err != nil {
		t.Fatalf("logout-others revoked current session: %v", err)
	}

	memberActor := AuthenticatedUser{ID: member.ID, DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	if _, err := service.UpdateAuthSettings(ctx, memberActor, settings); err == nil {
		t.Fatal("deployment member updated authentication settings")
	}
}
