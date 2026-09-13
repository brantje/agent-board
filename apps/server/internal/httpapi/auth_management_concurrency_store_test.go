package httpapi

import (
	"context"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *authHTTPStore) SetUserPasswordIfAuthVersion(_ context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	if user.AuthVersion != expectedAuthVersion {
		return store.User{}, store.ErrConflict
	}
	user.PasswordHash = passwordHash
	if user.Status == store.UserStatusPending {
		user.Status = store.UserStatusActive
	}
	user.ForcePasswordChange = force
	user.AuthVersion++
	s.users[id] = user
	now := time.Unix(2, 0).UTC()
	s.revokeUserSessionsLocked(id, now)
	for key, token := range s.tokens {
		if token.UserID == id && token.ConsumedAt == nil && token.RevokedAt == nil {
			token.RevokedAt = &now
			s.tokens[key] = token
		}
	}
	return user, nil
}

func (s *authHTTPStore) SetUserDisabled(_ context.Context, id string, disabled bool) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}

	targetStatus := store.UserStatusDisabled
	if !disabled {
		targetStatus = store.UserStatusActive
		if user.PasswordHash == "" {
			targetStatus = store.UserStatusPending
		}
	}
	if user.Status == targetStatus {
		return user, nil
	}
	if user.Status == store.UserStatusActive && user.DeploymentRole == store.DeploymentRoleAdmin && targetStatus != store.UserStatusActive {
		activeAdmins := 0
		for _, existing := range s.users {
			if existing.DeploymentRole == store.DeploymentRoleAdmin && existing.Status == store.UserStatusActive {
				activeAdmins++
			}
		}
		if activeAdmins <= 1 {
			return store.User{}, store.ErrConflict
		}
	}
	user.Status = targetStatus
	user.AuthVersion++
	s.users[id] = user
	s.revokeUserSessionsLocked(id, time.Unix(2, 0).UTC())
	return user, nil
}
