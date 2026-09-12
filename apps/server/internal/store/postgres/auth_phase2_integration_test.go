package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAuthPhase2StoreUserSessionAndSettingsManagement(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	admin := authUser("admin", "admin@example.com", store.UserStatusActive)
	admin.DeploymentRole = store.DeploymentRoleAdmin
	admin, err := s.BootstrapUser(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateUser(ctx, authUser("member", "member@example.com", store.UserStatusPending))
	if err != nil {
		t.Fatal(err)
	}

	users, err := s.ListUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("list users = %+v, %v", users, err)
	}
	updated, err := s.UpdateUserIdentity(ctx, member.ID, "member.updated", "member.updated@example.com", "Member Updated")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Username != "member.updated" || updated.Email != "member.updated@example.com" || updated.DisplayName != "Member Updated" {
		t.Fatalf("identity update = %+v", updated)
	}
	if _, err := s.UpdateUserIdentity(ctx, member.ID, admin.Email, "other@example.com", "Conflict"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-user identity conflict = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	hash := func(marker byte) []byte {
		value := make([]byte, 32)
		value[0] = marker
		return value
	}
	first, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: admin.ID, RefreshTokenHash: hash(31), ExpiresAt: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: admin.ID, RefreshTokenHash: hash(32), ExpiresAt: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: member.ID, RefreshTokenHash: hash(33), ExpiresAt: now.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListUserAuthSessions(ctx, admin.ID, now)
	if err != nil || len(listed) != 2 {
		t.Fatalf("admin sessions = %+v, %v", listed, err)
	}
	if err := s.RevokeAuthSession(ctx, admin.ID, first.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	listed, err = s.ListUserAuthSessions(ctx, admin.ID, now.Add(2*time.Minute))
	if err != nil || len(listed) != 1 || listed[0].ID != second.ID {
		t.Fatalf("sessions after single revoke = %+v, %v", listed, err)
	}
	if err := s.RevokeAuthSession(ctx, member.ID, second.ID, now.Add(2*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-user session revoke = %v", err)
	}
	third, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: admin.ID, RefreshTokenHash: hash(34), ExpiresAt: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeOtherAuthSessions(ctx, admin.ID, third.ID, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	listed, err = s.ListUserAuthSessions(ctx, admin.ID, now.Add(4*time.Minute))
	if err != nil || len(listed) != 1 || listed[0].ID != third.ID {
		t.Fatalf("sessions after revoke-others = %+v, %v", listed, err)
	}

	settings, err := s.GetAuthSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.AccessTokenLifetime = 2 * time.Hour
	settings.RefreshTokenLifetime = 14 * 24 * time.Hour
	settings.PasswordPolicy.MinimumLength = 16
	settings.PasswordPolicy.RequireUppercase = true
	settings.PasswordPolicy.RequireLowercase = true
	settings.PasswordPolicy.RequireNumber = true
	settings.PasswordPolicy.RequireSymbol = true
	stored, err := s.UpdateAuthSettings(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessTokenLifetime != 2*time.Hour || stored.RefreshTokenLifetime != 14*24*time.Hour || stored.PasswordPolicy.MinimumLength != 16 || !stored.PasswordPolicy.RequireSymbol {
		t.Fatalf("updated settings = %+v", stored)
	}
	reloaded, err := s.GetAuthSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded != stored {
		t.Fatalf("settings were not persisted: got %+v want %+v", reloaded, stored)
	}
}

func TestAuthPhase2StoreStatusAndPasswordChangesInvalidateCredentials(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	user, err := s.BootstrapUser(ctx, func() store.User {
		value := authUser("admin", "admin@example.com", store.UserStatusActive)
		value.DeploymentRole = store.DeploymentRoleAdmin
		value.PasswordHash = "initial-hash"
		return value
	}())
	if err != nil {
		t.Fatal(err)
	}
	backupAdmin := authUser("backup-admin", "backup-admin@example.com", store.UserStatusActive)
	backupAdmin.DeploymentRole = store.DeploymentRoleAdmin
	if _, err := s.CreateUser(ctx, backupAdmin); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	hash := func(marker byte) []byte {
		value := make([]byte, 32)
		value[0] = marker
		return value
	}

	unchanged, err := s.SetUserStatus(ctx, user.ID, store.UserStatusActive)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.AuthVersion != user.AuthVersion {
		t.Fatalf("no-op status change incremented auth version: got %d want %d", unchanged.AuthVersion, user.AuthVersion)
	}

	session, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: user.ID, RefreshTokenHash: hash(41), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := s.SetUserStatus(ctx, user.ID, store.UserStatusDisabled)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != store.UserStatusDisabled || disabled.AuthVersion != user.AuthVersion+1 {
		t.Fatalf("disabled user = %+v", disabled)
	}
	storedSession, err := s.GetAuthSessionByRefreshHash(ctx, session.RefreshTokenHash)
	if err != nil || storedSession.RevokedAt == nil {
		t.Fatalf("status change did not revoke session: %+v, %v", storedSession, err)
	}
	if _, err := s.SetUserStatus(ctx, "00000000-0000-0000-0000-000000000999", store.UserStatusDisabled); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing user status change = %v", err)
	}

	enabled, err := s.SetUserStatus(ctx, user.ID, store.UserStatusActive)
	if err != nil {
		t.Fatal(err)
	}
	if enabled.AuthVersion != disabled.AuthVersion+1 {
		t.Fatalf("re-enable auth version = %d want %d", enabled.AuthVersion, disabled.AuthVersion+1)
	}

	passwordSession, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: user.ID, RefreshTokenHash: hash(42), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	resetToken, err := s.CreatePasswordToken(ctx, store.PasswordToken{UserID: user.ID, Purpose: store.PasswordTokenPurposeReset, TokenHash: hash(43), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.SetUserPassword(ctx, user.ID, "replacement-hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed.PasswordHash != "replacement-hash" || !changed.ForcePasswordChange || changed.AuthVersion != enabled.AuthVersion+1 {
		t.Fatalf("password change = %+v", changed)
	}
	storedPasswordSession, err := s.GetAuthSessionByRefreshHash(ctx, passwordSession.RefreshTokenHash)
	if err != nil || storedPasswordSession.RevokedAt == nil {
		t.Fatalf("password change did not revoke session: %+v, %v", storedPasswordSession, err)
	}
	if _, err := s.CompletePasswordToken(ctx, resetToken.TokenHash, store.PasswordTokenPurposeReset, "ignored", now.Add(time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("password change did not revoke reset token: %v", err)
	}

	activeSession, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: user.ID, RefreshTokenHash: hash(44), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeUserAuthSessions(ctx, user.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	revokedSession, err := s.GetAuthSessionByRefreshHash(ctx, activeSession.RefreshTokenHash)
	if err != nil || revokedSession.RevokedAt == nil {
		t.Fatalf("bulk revoke did not revoke session: %+v, %v", revokedSession, err)
	}
}
