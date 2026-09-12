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
