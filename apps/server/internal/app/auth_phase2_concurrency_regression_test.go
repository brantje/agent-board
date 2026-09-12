package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type blockingPasswordMutationStore struct {
	*phase2LifecycleMemory
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingPasswordMutationStore() *blockingPasswordMutationStore {
	return &blockingPasswordMutationStore{
		phase2LifecycleMemory: &phase2LifecycleMemory{authMemory: newAuthMemory()},
		started:               make(chan struct{}),
		release:               make(chan struct{}),
	}
}

func (m *blockingPasswordMutationStore) waitForPasswordCommit() {
	m.once.Do(func() { close(m.started) })
	<-m.release
}

func (m *blockingPasswordMutationStore) SetUserPassword(ctx context.Context, id, passwordHash string, force bool) (store.User, error) {
	m.waitForPasswordCommit()
	return m.phase2LifecycleMemory.SetUserPassword(ctx, id, passwordHash, force)
}

// This method intentionally exists before the production store contract grows it.
// The regression proves ChangeOwnPassword actually selects the auth-version-aware
// mutation instead of the legacy unconditional write.
func (m *blockingPasswordMutationStore) SetUserPasswordIfAuthVersion(ctx context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	m.waitForPasswordCommit()
	current, err := m.phase2LifecycleMemory.GetUser(ctx, id)
	if err != nil {
		return store.User{}, err
	}
	if current.AuthVersion != expectedAuthVersion {
		return store.User{}, store.ErrConflict
	}
	return m.phase2LifecycleMemory.SetUserPassword(ctx, id, passwordHash, force)
}

func (m *blockingPasswordMutationStore) SetUserDisabled(ctx context.Context, id string, disabled bool) (store.User, error) {
	current, err := m.phase2LifecycleMemory.GetUser(ctx, id)
	if err != nil {
		return store.User{}, err
	}
	status := store.UserStatusDisabled
	if !disabled {
		status = store.UserStatusActive
		if current.PasswordHash == "" {
			status = store.UserStatusPending
		}
	}
	return m.phase2LifecycleMemory.SetUserStatus(ctx, id, status)
}

type casLifecycleMemory struct {
	*phase2LifecycleMemory
}

func (m *casLifecycleMemory) SetUserPasswordIfAuthVersion(ctx context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
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

func (m *casLifecycleMemory) SetUserDisabled(ctx context.Context, id string, disabled bool) (store.User, error) {
	current, err := m.phase2LifecycleMemory.GetUser(ctx, id)
	if err != nil {
		return store.User{}, err
	}
	status := store.UserStatusDisabled
	if !disabled {
		status = store.UserStatusActive
		if current.PasswordHash == "" {
			status = store.UserStatusPending
		}
	}
	return m.phase2LifecycleMemory.SetUserStatus(ctx, id, status)
}

func TestPhase2StaleSelfPasswordChangeCannotOverwriteAdminAssignment(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newBlockingPasswordMutationStore()
	service := phase2ReviewAuthTestService(t, memory, &now)
	admin := bootstrapTestUser(t, service)

	result := make(chan error, 1)
	go func() {
		_, err := service.ChangeOwnPassword(ctx, admin, "long-enough-password", "stale-self-password")
		result <- err
	}()

	<-memory.started
	recoveryHash, err := service.hashPassword("admin-recovery-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memory.phase2LifecycleMemory.SetUserPassword(ctx, admin.ID, recoveryHash, true); err != nil {
		t.Fatal(err)
	}
	close(memory.release)

	if err := <-result; !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale self password change error=%v want conflict", err)
	}
	stored, err := memory.GetUser(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if valid, _ := verifyPassword(stored.PasswordHash, "admin-recovery-password"); !valid {
		t.Fatal("admin recovery password was overwritten")
	}
	if valid, _ := verifyPassword(stored.PasswordHash, "stale-self-password"); valid {
		t.Fatal("stale self password unexpectedly became valid")
	}
}

func TestPhase2ForcedPasswordChangeUsesAuthenticatedAuthVersion(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := &casLifecycleMemory{phase2LifecycleMemory: &phase2LifecycleMemory{authMemory: newAuthMemory()}}
	service := phase2ReviewAuthTestService(t, memory, &now)
	admin := bootstrapTestUser(t, service)

	if _, err := service.AdminSetPassword(ctx, admin, admin.ID, "temporary-admin-password"); err != nil {
		t.Fatal(err)
	}
	forced, err := service.Login(ctx, "admin", "temporary-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if !forced.User.ForcePasswordChange {
		t.Fatal("temporary password login was not forced-change")
	}

	if _, err := service.AdminSetPassword(ctx, phase2Admin(), admin.ID, "admin-recovery-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ChangeOwnPassword(ctx, forced.User, "", "stale-forced-password"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale forced password change error=%v want conflict", err)
	}
	stored, err := memory.GetUser(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if valid, _ := verifyPassword(stored.PasswordHash, "admin-recovery-password"); !valid {
		t.Fatal("admin recovery password was overwritten by stale forced change")
	}
	if valid, _ := verifyPassword(stored.PasswordHash, "stale-forced-password"); valid {
		t.Fatal("stale forced password unexpectedly became valid")
	}
}
