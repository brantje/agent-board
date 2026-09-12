package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type failingAuthReader struct{}

func (failingAuthReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestAuthSettingsValidationBounds(t *testing.T) {
	valid := store.AuthSettings{
		AccessTokenLifetime:  time.Hour,
		RefreshTokenLifetime: 30 * 24 * time.Hour,
		PasswordPolicy:       store.PasswordPolicy{MinimumLength: 12},
	}
	if err := validateAuthSettings(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	cases := []store.AuthSettings{
		{AccessTokenLifetime: 4 * time.Minute, RefreshTokenLifetime: valid.RefreshTokenLifetime, PasswordPolicy: valid.PasswordPolicy},
		{AccessTokenLifetime: 25 * time.Hour, RefreshTokenLifetime: valid.RefreshTokenLifetime, PasswordPolicy: valid.PasswordPolicy},
		{AccessTokenLifetime: valid.AccessTokenLifetime, RefreshTokenLifetime: 59 * time.Minute, PasswordPolicy: valid.PasswordPolicy},
		{AccessTokenLifetime: valid.AccessTokenLifetime, RefreshTokenLifetime: 366 * 24 * time.Hour, PasswordPolicy: valid.PasswordPolicy},
		{AccessTokenLifetime: valid.AccessTokenLifetime, RefreshTokenLifetime: valid.RefreshTokenLifetime, PasswordPolicy: store.PasswordPolicy{MinimumLength: 7}},
		{AccessTokenLifetime: valid.AccessTokenLifetime, RefreshTokenLifetime: valid.RefreshTokenLifetime, PasswordPolicy: store.PasswordPolicy{MinimumLength: 257}},
	}
	for i, settings := range cases {
		if err := validateAuthSettings(settings); err == nil {
			t.Fatalf("invalid settings case %d accepted: %#v", i, settings)
		}
	}
}

func TestNormalizeIdentityRejectsMissingAndInvalidEmail(t *testing.T) {
	for _, input := range []struct {
		username string
		email    string
		display  string
	}{
		{"", "admin@example.com", "Admin"},
		{"admin", "", "Admin"},
		{"admin", "admin@example.com", ""},
		{"admin", "not-an-email", "Admin"},
		{"admin", "Admin <admin@example.com>", "Admin"},
	} {
		if _, _, _, err := normalizeIdentity(input.username, input.email, input.display); err == nil {
			t.Fatalf("invalid identity accepted: %#v", input)
		}
	}
}

func TestOpaqueTokenValidationRejectsMalformedSecrets(t *testing.T) {
	for _, token := range []string{"", "not-base64!", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, randomTokenBytes-1))} {
		if _, err := hashOpaqueToken(token); err == nil {
			t.Fatalf("malformed token accepted: %q", token)
		}
	}
	valid := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, randomTokenBytes))
	if hash, err := hashOpaqueToken("  " + valid + "  "); err != nil || len(hash) != 32 {
		t.Fatalf("valid token hash = %x, %v", hash, err)
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	bad := []string{
		"plain-text",
		"$argon2id$v=1$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$bad-params$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$m=1,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$bad!$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$bad!",
	}
	for _, encoded := range bad {
		if ok, err := verifyPassword(encoded, "password"); err == nil || ok {
			t.Fatalf("malformed password hash accepted: %q", encoded)
		}
	}
}

func TestAccessTokenVerificationRejectsMalformedTokens(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	for _, token := range []string{"", "a.b", "!.payload.signature"} {
		if _, err := verifyAccessToken(key, token); err == nil {
			t.Fatalf("malformed access token accepted: %q", token)
		}
	}

	claims := accessClaims{Subject: "user", AuthVersion: 1, Issuer: authIssuer, Audience: authAudience, IssuedAt: 1, ExpiresAt: 2}
	token, err := signAccessToken(key, claims)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	parts[2] = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	if _, err := verifyAccessToken(key, strings.Join(parts, ".")); err == nil {
		t.Fatal("access token with invalid signature accepted")
	}
}

func TestAuthServiceRejectsInvalidInputsAndRandomFailures(t *testing.T) {
	if _, err := NewAuthService(nil, AuthServiceConfig{}); err == nil {
		t.Fatal("nil auth store accepted")
	}
	if _, err := NewAuthService(newAuthMemory(), AuthServiceConfig{Random: failingAuthReader{}}); err == nil {
		t.Fatal("signing key generation failure ignored")
	}

	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	service := authTestService(t, memory, &now)
	user := bootstrapTestUser(t, service)

	if _, err := service.Login(context.Background(), "", "password"); err == nil {
		t.Fatal("empty login accepted")
	}
	if _, err := service.CreatePasswordToken(context.Background(), user.ID, "invalid"); err == nil {
		t.Fatal("invalid password-token purpose accepted")
	}
	if _, err := service.CompletePasswordToken(context.Background(), "token", "invalid", "long-enough-password"); err == nil {
		t.Fatal("invalid completion purpose accepted")
	}
	if _, err := service.SetStatus(context.Background(), user.ID, "deleted"); err == nil {
		t.Fatal("invalid user status accepted")
	}
	if err := service.Logout(context.Background(), "not-a-token"); err != nil {
		t.Fatalf("invalid logout token should be idempotent: %v", err)
	}
	if _, err := service.AuthenticateAccessToken(context.Background(), "not-a-jwt"); err == nil {
		t.Fatal("invalid access token authenticated")
	}

	memory.settings.AccessTokenLifetime = time.Minute
	if _, err := service.Login(context.Background(), "admin", "long-enough-password"); err == nil {
		t.Fatal("login ignored invalid auth settings")
	}

	broken, err := NewAuthService(memory, AuthServiceConfig{
		Now:        func() time.Time { return now },
		Random:     failingAuthReader{},
		SigningKey: bytes.Repeat([]byte{7}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	memory.settings = store.AuthSettings{AccessTokenLifetime: time.Hour, RefreshTokenLifetime: 30 * 24 * time.Hour, PasswordPolicy: store.PasswordPolicy{MinimumLength: 12}}
	if _, err := broken.CreatePasswordToken(context.Background(), user.ID, store.PasswordTokenPurposeReset); err == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("token random failure = %v", err)
	}
	if _, err := broken.SetPassword(context.Background(), user.ID, "another-long-password", false); err == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("password salt failure = %v", err)
	}
}
