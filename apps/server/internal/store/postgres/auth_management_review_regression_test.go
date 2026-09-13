package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAuthStorePendingDirectPasswordIsAtomicLifecycleTransition(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	pending, err := s.CreateUser(ctx, authUser("member", "member@example.com", store.UserStatusPending))
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
		UserID: pending.ID, RefreshTokenHash: hash(61), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := s.CreatePasswordToken(ctx, store.PasswordToken{
		UserID: pending.ID, Purpose: store.PasswordTokenPurposeSetup, TokenHash: hash(62), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	changed, err := s.SetUserPassword(ctx, pending.ID, "temporary-hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Status != store.UserStatusActive || changed.PasswordHash != "temporary-hash" || !changed.ForcePasswordChange || changed.AuthVersion != pending.AuthVersion+1 {
		t.Fatalf("pending direct password transition = %+v", changed)
	}
	storedSession, err := s.GetAuthSessionByRefreshHash(ctx, session.RefreshTokenHash)
	if err != nil || storedSession.RevokedAt == nil {
		t.Fatalf("pending direct password did not revoke session: %+v, %v", storedSession, err)
	}
	if _, err := s.CompletePasswordToken(ctx, setup.TokenHash, store.PasswordTokenPurposeSetup, "replacement-hash", now.Add(time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pending direct password did not revoke setup token: %v", err)
	}
}
