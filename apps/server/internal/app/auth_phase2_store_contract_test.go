package app

import (
	"context"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (m *authMemory) SetUserPasswordIfAuthVersion(_ context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	if user.AuthVersion != expectedAuthVersion {
		return store.User{}, store.ErrConflict
	}
	user.PasswordHash = passwordHash
	user.ForcePasswordChange = force
	user.AuthVersion++
	m.users[id] = user
	m.revokeUserSessionsLocked(id, time.Unix(2, 0).UTC())
	return user, nil
}

func (m *authMemory) SetUserDisabled(_ context.Context, id string, disabled bool) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	status := store.UserStatusDisabled
	if !disabled {
		status = store.UserStatusActive
		if user.PasswordHash == "" {
			status = store.UserStatusPending
		}
	}
	if user.Status != status {
		user.Status = status
		user.AuthVersion++
		m.users[id] = user
		m.revokeUserSessionsLocked(id, time.Unix(2, 0).UTC())
	}
	return user, nil
}

func (m *phase2LifecycleMemory) SetUserPasswordIfAuthVersion(_ context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
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
	m.users[id] = user
	now := time.Unix(2, 0).UTC()
	m.revokeUserSessionsLocked(id, now)
	for key, token := range m.tokens {
		if token.UserID == id && token.ConsumedAt == nil && token.RevokedAt == nil {
			token.RevokedAt = &now
			m.tokens[key] = token
		}
	}
	return user, nil
}

func (m *phase2LifecycleMemory) SetUserDisabled(ctx context.Context, id string, disabled bool) (store.User, error) {
	return m.authMemory.SetUserDisabled(ctx, id, disabled)
}
