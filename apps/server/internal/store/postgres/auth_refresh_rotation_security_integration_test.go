package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func securityRefreshHash(marker byte) []byte {
	hash := make([]byte, 32)
	hash[0] = marker
	return hash
}

func securityAuthAdmin(t *testing.T, s *Store) store.User {
	t.Helper()
	user := authUser("security-admin", "security-admin@example.com", store.UserStatusActive)
	user.DeploymentRole = store.DeploymentRoleAdmin
	created, err := s.BootstrapUser(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestAuthStoreOldRefreshGenerationRevokesCurrentLogicalSession(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := t.Context()
	user := securityAuthAdmin(t, s)
	now := time.Now().UTC().Truncate(time.Second)

	firstHash := securityRefreshHash(21)
	secondHash := securityRefreshHash(22)
	thirdHash := securityRefreshHash(23)
	fourthHash := securityRefreshHash(24)
	unrelatedHash := securityRefreshHash(25)

	session, err := s.CreateAuthSession(ctx, store.AuthSession{
		UserID:           user.ID,
		RefreshTokenHash: firstHash,
		ExpiresAt:        now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := s.CreateAuthSession(ctx, store.AuthSession{
		UserID:           user.ID,
		RefreshTokenHash: unrelatedHash,
		ExpiresAt:        now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := s.RotateAuthSession(ctx, session.ID, firstHash, secondHash, session.ExpiresAt, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("rotate A to B: %v", err)
	}
	rotated, err = s.RotateAuthSession(ctx, session.ID, secondHash, thirdHash, rotated.ExpiresAt, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("rotate B to C: %v", err)
	}
	if _, err := s.GetAuthSessionByRefreshHash(ctx, firstHash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed refresh generation remained refreshable: %v", err)
	}
	var generations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth_session_refresh_tokens WHERE session_id=$1`, session.ID).Scan(&generations); err != nil {
		t.Fatal(err)
	}
	if generations != 3 {
		t.Fatalf("retained refresh generations=%d want 3", generations)
	}

	if err := s.RevokeAuthSessionByRefreshHash(ctx, firstHash, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("logout by consumed A: %v", err)
	}
	current, err := s.GetAuthSessionByRefreshHash(ctx, thirdHash)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != session.ID || current.RevokedAt == nil {
		t.Fatalf("logical session was not revoked: %#v", current)
	}
	if _, err := s.RotateAuthSession(ctx, session.ID, thirdHash, fourthHash, current.ExpiresAt, now.Add(4*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked current generation rotated: %v", err)
	}

	other, err := s.GetAuthSessionByRefreshHash(ctx, unrelatedHash)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID != unrelated.ID || other.RevokedAt != nil {
		t.Fatalf("unrelated session changed: %#v", other)
	}
}

func TestAuthStoreLogoutBeforeRefreshPreventsRotation(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	user := securityAuthAdmin(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	firstHash := securityRefreshHash(31)
	secondHash := securityRefreshHash(32)

	session, err := s.CreateAuthSession(ctx, store.AuthSession{
		UserID:           user.ID,
		RefreshTokenHash: firstHash,
		ExpiresAt:        now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAuthSessionByRefreshHash(ctx, firstHash, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RotateAuthSession(ctx, session.ID, firstHash, secondHash, session.ExpiresAt, now.Add(2*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refresh after logout error=%v want not found", err)
	}
}

func TestAuthStoreConcurrentRefreshHasSingleWinner(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := t.Context()
	user := securityAuthAdmin(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	firstHash := securityRefreshHash(41)
	candidateHashes := [][]byte{securityRefreshHash(42), securityRefreshHash(43)}

	session, err := s.CreateAuthSession(ctx, store.AuthSession{
		UserID:           user.ID,
		RefreshTokenHash: firstHash,
		ExpiresAt:        now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, len(candidateHashes))
	var wg sync.WaitGroup
	for _, candidate := range candidateHashes {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.RotateAuthSession(ctx, session.ID, firstHash, candidate, session.ExpiresAt, now.Add(time.Minute))
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	rejected := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrNotFound):
			rejected++
		default:
			t.Fatalf("unexpected refresh result: %v", err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("concurrent refresh successes=%d rejected=%d want 1/1", successes, rejected)
	}

	currentGenerations := 0
	for _, candidate := range candidateHashes {
		if _, err := s.GetAuthSessionByRefreshHash(ctx, candidate); err == nil {
			currentGenerations++
		} else if !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if currentGenerations != 1 {
		t.Fatalf("current refresh generations=%d want 1", currentGenerations)
	}
	var generations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth_session_refresh_tokens WHERE session_id=$1`, session.ID).Scan(&generations); err != nil {
		t.Fatal(err)
	}
	if generations != 2 {
		t.Fatalf("retained refresh generations=%d want initial plus single winner", generations)
	}
}
