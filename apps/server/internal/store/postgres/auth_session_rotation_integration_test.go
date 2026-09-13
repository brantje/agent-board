package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func newPostgresAuthTestService(t *testing.T, s *Store, now *time.Time) *app.AuthService {
	t.Helper()
	service, err := app.NewAuthService(s, app.AuthServiceConfig{
		Now:        func() time.Time { return *now },
		SigningKey: bytes.Repeat([]byte{9}, 32),
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return service
}

func bootstrapPostgresAuthTestUser(t *testing.T, service *app.AuthService) app.AuthenticatedUser {
	t.Helper()
	user, err := service.Bootstrap(t.Context(), app.BootstrapRegistration{
		Username:    "admin",
		Email:       "admin@example.com",
		DisplayName: "Admin",
		Password:    "long-enough-password",
	})
	if err != nil {
		t.Fatalf("bootstrap auth user: %v", err)
	}
	return user
}

func TestAuthSessionLogoutRecognizesEveryRotatedRefreshGeneration(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	now := time.Now().UTC()
	service := newPostgresAuthTestService(t, s, &now)
	bootstrapPostgresAuthTestUser(t, service)

	first, err := service.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := service.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}

	// Keep last_used_at after the database-created session timestamp.
	now = now.Add(time.Minute)
	second, err := service.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("refresh A -> B: %v", err)
	}
	if _, err := service.Refresh(ctx, first.RefreshToken); err == nil {
		t.Fatal("consumed refresh generation A was reusable")
	}

	now = now.Add(time.Minute)
	third, err := service.Refresh(ctx, second.RefreshToken)
	if err != nil {
		t.Fatalf("refresh B -> C: %v", err)
	}
	if _, err := service.Refresh(ctx, second.RefreshToken); err == nil {
		t.Fatal("consumed refresh generation B was reusable")
	}

	firstHash := sha256.Sum256([]byte(first.RefreshToken))
	thirdHash := sha256.Sum256([]byte(third.RefreshToken))
	currentSession, err := s.GetAuthSessionByRefreshHash(ctx, thirdHash[:])
	if err != nil {
		t.Fatalf("current session lookup: %v", err)
	}
	if _, err := s.GetAuthSessionByRefreshHash(ctx, firstHash[:]); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old refresh generation remained refreshable: %v", err)
	}
	var generationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth_session_refresh_tokens WHERE session_id=$1`, currentSession.ID).Scan(&generationCount); err != nil {
		t.Fatal(err)
	}
	if generationCount != 3 {
		t.Fatalf("stored refresh generations=%d want 3", generationCount)
	}

	// This ordering deterministically reproduces the old defect: rotation has
	// committed before logout receives A. A must still identify the same session.
	now = now.Add(time.Minute)
	if err := service.Logout(ctx, first.RefreshToken); err != nil {
		t.Fatalf("logout with A after rotations: %v", err)
	}
	stored, err := s.GetAuthSessionByRefreshHash(ctx, thirdHash[:])
	if err != nil {
		t.Fatalf("current session after logout: %v", err)
	}
	if stored.ID != currentSession.ID || stored.RevokedAt == nil {
		t.Fatalf("logical session was not revoked: %#v", stored)
	}
	if _, err := service.Refresh(ctx, third.RefreshToken); err == nil {
		t.Fatal("current refresh generation C survived logout(A)")
	}

	if _, err := service.Refresh(ctx, unrelated.RefreshToken); err != nil {
		t.Fatalf("unrelated session was revoked: %v", err)
	}
}

func TestAuthSessionLogoutBeforeRefreshPreventsRotation(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	now := time.Now().UTC()
	service := newPostgresAuthTestService(t, s, &now)
	bootstrapPostgresAuthTestUser(t, service)

	tokens, err := service.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := service.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(ctx, tokens.RefreshToken); err == nil {
		t.Fatal("refresh succeeded after logout won the race")
	}
}

func TestAuthSessionConcurrentRefreshHasSingleWinner(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	now := time.Now().UTC()
	service := newPostgresAuthTestService(t, s, &now)
	bootstrapPostgresAuthTestUser(t, service)

	tokens, err := service.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Refresh(ctx, tokens.RefreshToken)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	failures := 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent refresh results successes=%d failures=%d want 1/1", successes, failures)
	}
}

func TestAuthSessionLogoutDoesNotInvalidateIssuedAccessToken(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	now := time.Now().UTC()
	service := newPostgresAuthTestService(t, s, &now)
	user := bootstrapPostgresAuthTestUser(t, service)

	tokens, err := service.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := service.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(ctx, tokens.RefreshToken); err == nil {
		t.Fatal("logged-out refresh credential remained usable")
	}

	// Ordinary session revocation is intentionally refresh-session scoped. Access
	// JWTs remain valid until expiry unless authoritative User state invalidates them.
	if authenticated, err := service.AuthenticateAccessToken(ctx, tokens.AccessToken); err != nil || authenticated.ID != user.ID {
		t.Fatalf("issued access token after logout = %#v, %v", authenticated, err)
	}
	if _, err := service.SetPassword(ctx, user.ID, "another-long-password", false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateAccessToken(ctx, tokens.AccessToken); err == nil {
		t.Fatal("access token survived independent auth-version invalidation")
	}
}
