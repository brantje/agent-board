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

func (m *blockingPasswordMutationStore) SetUserPasswordIfAuthVersion(ctx context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	m.waitForPasswordCommit()
	return m.phase2LifecycleMemory.SetUserPasswordIfAuthVersion(ctx, id, expectedAuthVersion, passwordHash, force)
}

func (m *blockingPasswordMutationStore) SetUserDisabled(ctx context.Context, id string, disabled bool) (store.User, error) {
	return m.phase2LifecycleMemory.SetUserDisabled(ctx, id, disabled)
}

type casLifecycleMemory struct {
	*phase2LifecycleMemory
}

func (m *casLifecycleMemory) SetUserPasswordIfAuthVersion(ctx context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	return m.phase2LifecycleMemory.SetUserPasswordIfAuthVersion(ctx, id, expectedAuthVersion, passwordHash, force)
}

func (m *casLifecycleMemory) SetUserDisabled(ctx context.Context, id string, disabled bool) (store.User, error) {
	return m.phase2LifecycleMemory.SetUserDisabled(ctx, id, disabled)
}

type blockingCASLifecycleMemory struct {
	*casLifecycleMemory
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingCASLifecycleMemory() *blockingCASLifecycleMemory {
	return &blockingCASLifecycleMemory{
		casLifecycleMemory: &casLifecycleMemory{phase2LifecycleMemory: &phase2LifecycleMemory{authMemory: newAuthMemory()}},
		started:            make(chan struct{}),
		release:            make(chan struct{}),
	}
}

func (m *blockingCASLifecycleMemory) SetUserPasswordIfAuthVersion(ctx context.Context, id string, expectedAuthVersion int64, passwordHash string, force bool) (store.User, error) {
	m.once.Do(func() { close(m.started) })
	<-m.release
	return m.casLifecycleMemory.SetUserPasswordIfAuthVersion(ctx, id, expectedAuthVersion, passwordHash, force)
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

func TestPhase2ForcedPasswordChangeUsesCapturedAuthVersion(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newBlockingCASLifecycleMemory()
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

	result := make(chan error, 1)
	go func() {
		_, err := service.ChangeOwnPassword(ctx, forced.User, "", "stale-forced-password")
		result <- err
	}()
	<-memory.started

	if _, err := service.AdminSetPassword(ctx, phase2Admin(), admin.ID, "admin-recovery-password"); err != nil {
		t.Fatal(err)
	}
	close(memory.release)
	if err := <-result; !errors.Is(err, store.ErrConflict) {
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
