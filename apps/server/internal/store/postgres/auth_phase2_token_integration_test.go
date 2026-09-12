package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAuthPhase2StorePasswordTokenLifecycle(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	admin := authUser("admin", "admin@example.com", store.UserStatusActive)
	admin.DeploymentRole = store.DeploymentRoleAdmin
	admin.PasswordHash = "initial-hash"
	admin, err := s.BootstrapUser(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	hash := func(marker byte) []byte {
		value := make([]byte, 32)
		value[0] = marker
		return value
	}

	session, err := s.CreateAuthSession(ctx, store.AuthSession{
		UserID: admin.ID, RefreshTokenHash: hash(51), ExpiresAt: now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	firstReset, err := s.CreatePasswordToken(ctx, store.PasswordToken{
		UserID: admin.ID, Purpose: store.PasswordTokenPurposeReset, TokenHash: hash(52), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondReset, err := s.CreatePasswordToken(ctx, store.PasswordToken{
		UserID: admin.ID, Purpose: store.PasswordTokenPurposeReset, TokenHash: hash(53), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompletePasswordToken(ctx, firstReset.TokenHash, store.PasswordTokenPurposeReset, "old-reset-hash", now.Add(time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("replaced reset token should be unusable: %v", err)
	}
	if _, err := s.CompletePasswordToken(ctx, secondReset.TokenHash, store.PasswordTokenPurposeSetup, "wrong-purpose-hash", now.Add(time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token with wrong purpose should be unusable: %v", err)
	}

	resetUser, err := s.CompletePasswordToken(ctx, secondReset.TokenHash, store.PasswordTokenPurposeReset, "reset-password-hash", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if resetUser.PasswordHash != "reset-password-hash" || resetUser.ForcePasswordChange || resetUser.AuthVersion != admin.AuthVersion+1 {
		t.Fatalf("unexpected reset user: %+v", resetUser)
	}
	storedSession, err := s.GetAuthSessionByRefreshHash(ctx, session.RefreshTokenHash)
	if err != nil || storedSession.RevokedAt == nil {
		t.Fatalf("reset completion did not revoke session: %+v, %v", storedSession, err)
	}
	if _, err := s.CompletePasswordToken(ctx, secondReset.TokenHash, store.PasswordTokenPurposeReset, "reuse-hash", now.Add(3*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed reset token should be one-time: %v", err)
	}

	refreshSession, err := s.CreateAuthSession(ctx, store.AuthSession{
		UserID: admin.ID, RefreshTokenHash: hash(54), ExpiresAt: now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAuthSessionByRefreshHash(ctx, refreshSession.RefreshTokenHash, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAuthSessionByRefreshHash(ctx, refreshSession.RefreshTokenHash, now.Add(5*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second refresh-session revoke should report not found: %v", err)
	}

	pending, err := s.CreateUser(ctx, authUser("pending", "pending@example.com", store.UserStatusPending))
	if err != nil {
		t.Fatal(err)
	}
	setup, err := s.CreatePasswordToken(ctx, store.PasswordToken{
		UserID: pending.ID, Purpose: store.PasswordTokenPurposeSetup, TokenHash: hash(55), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	activated, err := s.CompletePasswordToken(ctx, setup.TokenHash, store.PasswordTokenPurposeSetup, "initial-member-hash", now.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if activated.Status != store.UserStatusActive || activated.PasswordHash != "initial-member-hash" || activated.ForcePasswordChange || activated.AuthVersion != pending.AuthVersion+1 {
		t.Fatalf("unexpected activated user: %+v", activated)
	}
	if _, err := s.CompletePasswordToken(ctx, setup.TokenHash, store.PasswordTokenPurposeSetup, "reuse-setup-hash", now.Add(7*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed setup token should be one-time: %v", err)
	}

	expired, err := s.CreatePasswordToken(ctx, store.PasswordToken{
		UserID: activated.ID, Purpose: store.PasswordTokenPurposeReset, TokenHash: hash(56), ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompletePasswordToken(ctx, expired.TokenHash, store.PasswordTokenPurposeReset, "expired-hash", now.Add(11*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired reset token should be unusable: %v", err)
	}
}
