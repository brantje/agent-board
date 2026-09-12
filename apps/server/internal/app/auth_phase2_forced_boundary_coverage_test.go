package app

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestPhase2ForcedPasswordActorRejectedAcrossNormalManagementOperations(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	service := authTestService(t, newAuthMemory(), &now)
	ctx := context.Background()
	actor := AuthenticatedUser{
		ID:                  "forced-admin",
		DeploymentRole:      store.DeploymentRoleAdmin,
		Status:              store.UserStatusActive,
		ForcePasswordChange: true,
	}

	assertPasswordChangeRequired := func(name string, err error) {
		t.Helper()
		apiErr, ok := AsError(err)
		if !ok || apiErr.Code != "password_change_required" {
			t.Fatalf("%s error = %v, want password_change_required", name, err)
		}
	}

	_, err := service.ListUsers(ctx, actor)
	assertPasswordChangeRequired("list users", err)

	_, err = service.CreatePendingUser(ctx, actor, PendingUserRegistration{
		Username: "member", Email: "member@example.com", DisplayName: "Member",
	})
	assertPasswordChangeRequired("create pending user", err)

	_, err = service.AdminCreatePasswordToken(ctx, actor, "member", store.PasswordTokenPurposeReset)
	assertPasswordChangeRequired("create password token", err)

	_, err = service.AdminSetPassword(ctx, actor, "member", "long-enough-password")
	assertPasswordChangeRequired("set user password", err)

	_, err = service.AdminSetDisabled(ctx, actor, "member", true)
	assertPasswordChangeRequired("disable user", err)

	_, err = service.UpdateOwnProfile(ctx, actor, UserProfileUpdate{
		Username: "forced", Email: "forced@example.com", DisplayName: "Forced",
	})
	assertPasswordChangeRequired("update profile", err)

	_, err = service.ListOwnSessions(ctx, actor)
	assertPasswordChangeRequired("list sessions", err)

	err = service.RevokeOwnSession(ctx, actor, "session")
	assertPasswordChangeRequired("revoke session", err)

	err = service.LogoutOtherSessions(ctx, actor, "refresh")
	assertPasswordChangeRequired("logout other sessions", err)

	_, err = service.AuthSettings(ctx, actor)
	assertPasswordChangeRequired("read settings", err)

	_, err = service.UpdateAuthSettings(ctx, actor, store.AuthSettings{
		AccessTokenLifetime:  time.Hour,
		RefreshTokenLifetime: 30 * 24 * time.Hour,
		PasswordPolicy:       store.PasswordPolicy{MinimumLength: 12},
	})
	assertPasswordChangeRequired("update settings", err)
}
