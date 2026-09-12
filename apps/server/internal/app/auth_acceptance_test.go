package app

import (
	"context"
	"testing"
	"time"
)

func TestNormalizeIdentityIsDeterministic(t *testing.T) {
	usernameA, emailA, displayA, err := normalizeIdentity(" Admin ", " Admin@Example.COM ", " Administrator ")
	if err != nil {
		t.Fatal(err)
	}
	usernameB, emailB, displayB, err := normalizeIdentity("admin", "admin@example.com", "Administrator")
	if err != nil {
		t.Fatal(err)
	}
	if usernameA != usernameB || emailA != emailB || displayA != displayB {
		t.Fatalf("normalization differed: (%q,%q,%q) != (%q,%q,%q)", usernameA, emailA, displayA, usernameB, emailB, displayB)
	}
}

func TestRefreshExpirationIsEnforced(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	bootstrapTestUser(t, service)
	tokens, err := service.Login(context.Background(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}

	now = tokens.RefreshTokenExpiresAt.Add(time.Second)
	if _, err := service.Refresh(context.Background(), tokens.RefreshToken); err == nil {
		t.Fatal("expired refresh token succeeded")
	}
}

func TestForcedPasswordChangeStateIsReturnedToClient(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	user := bootstrapTestUser(t, service)
	if _, err := service.SetPassword(context.Background(), user.ID, "replacement-password", true); err != nil {
		t.Fatal(err)
	}
	tokens, err := service.Login(context.Background(), "admin", "replacement-password")
	if err != nil {
		t.Fatal(err)
	}
	if !tokens.User.ForcePasswordChange {
		t.Fatal("forced-password-change state was not represented in login response")
	}
}
