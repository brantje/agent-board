package app

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestAccessTokenUsesDeploymentStableSigningKeyAcrossInstances(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	deploymentKey := bytes.Repeat([]byte{21}, 32)

	issuer, err := NewAuthService(memory, AuthServiceConfig{
		Now:        func() time.Time { return now },
		Random:     &deterministicReader{},
		SigningKey: deploymentKey,
	})
	if err != nil {
		t.Fatalf("construct issuer: %v", err)
	}
	user := bootstrapTestUser(t, issuer)
	tokens, err := issuer.Login(context.Background(), "admin", "long-enough-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Reconstructing the auth service with the same deployment configuration
	// models a normal server restart.
	restarted, err := NewAuthService(memory, AuthServiceConfig{
		Now:        func() time.Time { return now },
		SigningKey: deploymentKey,
	})
	if err != nil {
		t.Fatalf("construct restarted service: %v", err)
	}
	if got, err := restarted.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err != nil || got.ID != user.ID {
		t.Fatalf("token did not survive service reconstruction: user=%#v err=%v", got, err)
	}

	// A separately constructed service using the same deployment key models a
	// second server process/replica validating a token issued by the first.
	peer, err := NewAuthService(memory, AuthServiceConfig{
		Now:        func() time.Time { return now },
		SigningKey: append([]byte(nil), deploymentKey...),
	})
	if err != nil {
		t.Fatalf("construct peer service: %v", err)
	}
	if got, err := peer.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err != nil || got.ID != user.ID {
		t.Fatalf("peer did not validate issuer token: user=%#v err=%v", got, err)
	}

	otherDeployment, err := NewAuthService(memory, AuthServiceConfig{
		Now:        func() time.Time { return now },
		SigningKey: bytes.Repeat([]byte{22}, 32),
	})
	if err != nil {
		t.Fatalf("construct other-deployment service: %v", err)
	}
	if _, err := otherDeployment.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err == nil {
		t.Fatal("different signing key unexpectedly validated token")
	}
}
