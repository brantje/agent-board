package app

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type refreshLineageAuthStore struct {
	*authMemory
	lineageMu sync.Mutex
	lineage   map[string]string
}

func newRefreshLineageAuthStore() *refreshLineageAuthStore {
	return &refreshLineageAuthStore{
		authMemory: newAuthMemory(),
		lineage:    map[string]string{},
	}
}

func (s *refreshLineageAuthStore) CreateAuthSession(ctx context.Context, session store.AuthSession) (store.AuthSession, error) {
	created, err := s.authMemory.CreateAuthSession(ctx, session)
	if err != nil {
		return store.AuthSession{}, err
	}
	s.lineageMu.Lock()
	s.lineage[hashKey(session.RefreshTokenHash)] = created.ID
	s.lineageMu.Unlock()
	return created, nil
}

func (s *refreshLineageAuthStore) RotateAuthSession(ctx context.Context, id string, oldHash, newHash []byte, expiresAt, now time.Time) (store.AuthSession, error) {
	rotated, err := s.authMemory.RotateAuthSession(ctx, id, oldHash, newHash, expiresAt, now)
	if err != nil {
		return store.AuthSession{}, err
	}
	s.lineageMu.Lock()
	s.lineage[hashKey(newHash)] = rotated.ID
	s.lineageMu.Unlock()
	return rotated, nil
}

func (s *refreshLineageAuthStore) RevokeAuthSessionByRefreshHash(_ context.Context, hash []byte, now time.Time) error {
	s.lineageMu.Lock()
	sessionID, ok := s.lineage[hashKey(hash)]
	s.lineageMu.Unlock()
	if !ok {
		return store.ErrNotFound
	}

	s.authMemory.mu.Lock()
	defer s.authMemory.mu.Unlock()
	for key, session := range s.authMemory.sessions {
		if session.ID != sessionID {
			continue
		}
		if session.RevokedAt != nil {
			return store.ErrNotFound
		}
		session.RevokedAt = &now
		s.authMemory.sessions[key] = session
		return nil
	}
	return store.ErrNotFound
}

func authServiceWithRefreshLineage(t *testing.T, now *time.Time) *AuthService {
	t.Helper()
	service, err := NewAuthService(newRefreshLineageAuthStore(), AuthServiceConfig{
		Now:        func() time.Time { return *now },
		Random:     &deterministicReader{},
		SigningKey: bytes.Repeat([]byte{9}, 32),
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return service
}

func TestLogoutByRotatedRefreshGenerationRevokesLogicalSession(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service := authServiceWithRefreshLineage(t, &now)
	bootstrapTestUser(t, service)

	first, err := service.Login(context.Background(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	independent, err := service.Login(context.Background(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}

	secondGeneration, err := service.Refresh(context.Background(), first.RefreshToken)
	if err != nil {
		t.Fatalf("first rotation: %v", err)
	}
	if _, err := service.Refresh(context.Background(), first.RefreshToken); err == nil {
		t.Fatal("consumed first refresh generation replayed successfully")
	}
	thirdGeneration, err := service.Refresh(context.Background(), secondGeneration.RefreshToken)
	if err != nil {
		t.Fatalf("second rotation: %v", err)
	}
	if _, err := service.Refresh(context.Background(), secondGeneration.RefreshToken); err == nil {
		t.Fatal("consumed second refresh generation replayed successfully")
	}

	if err := service.Logout(context.Background(), first.RefreshToken); err != nil {
		t.Fatalf("logout by oldest refresh generation: %v", err)
	}
	if _, err := service.Refresh(context.Background(), thirdGeneration.RefreshToken); err == nil {
		t.Fatal("current refresh generation survived logout by an ancestor token")
	}
	if _, err := service.Refresh(context.Background(), independent.RefreshToken); err != nil {
		t.Fatalf("unrelated session was revoked: %v", err)
	}
}

func TestLogoutRevokesRefreshSessionButNotIssuedAccessToken(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service := authServiceWithRefreshLineage(t, &now)
	user := bootstrapTestUser(t, service)
	tokens, err := service.Login(context.Background(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Logout(context.Background(), tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), tokens.RefreshToken); err == nil {
		t.Fatal("logged-out refresh credential remained usable")
	}
	if authenticated, err := service.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err != nil || authenticated.ID != user.ID {
		t.Fatalf("ordinary logout invalidated already-issued access token: user=%#v err=%v", authenticated, err)
	}

	now = tokens.AccessTokenExpiresAt.Add(time.Second)
	if _, err := service.AuthenticateAccessToken(context.Background(), tokens.AccessToken); err == nil {
		t.Fatal("access token remained usable after expiry")
	}
}
