package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type authVersionPasswordMutator interface {
	SetUserPasswordIfAuthVersion(context.Context, string, int64, string, bool) (store.User, error)
}

type disabledUserMutator interface {
	SetUserDisabled(context.Context, string, bool) (store.User, error)
}

func TestAuthPhase2PasswordCASRejectsStaleAuthorizationState(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	user := authUser("member", "member@example.com", store.UserStatusActive)
	user.PasswordHash = "old-hash"
	user, err := s.CreateUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	expected := user.AuthVersion

	conditional, ok := any(s).(authVersionPasswordMutator)
	if !ok {
		t.Fatal("postgres auth store does not implement auth-version conditional password mutation")
	}
	if _, err := s.SetUserPassword(ctx, user.ID, "admin-recovery-hash", true); err != nil {
		t.Fatal(err)
	}
	if _, err := conditional.SetUserPasswordIfAuthVersion(ctx, user.ID, expected, "stale-self-hash", false); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale conditional password mutation error=%v want conflict", err)
	}
	stored, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PasswordHash != "admin-recovery-hash" {
		t.Fatalf("password hash=%q want admin recovery hash", stored.PasswordHash)
	}
}

func TestAuthPhase2ConcurrentPasswordCASAllowsOnlyOneWinner(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	user := authUser("concurrent-member", "concurrent-member@example.com", store.UserStatusActive)
	user.PasswordHash = "old-hash"
	user, err := s.CreateUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	conditional, ok := any(s).(authVersionPasswordMutator)
	if !ok {
		t.Fatal("postgres auth store does not implement auth-version conditional password mutation")
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, hash := range []string{"first-new-hash", "second-new-hash"} {
		wg.Add(1)
		go func(passwordHash string) {
			defer wg.Done()
			<-start
			_, err := conditional.SetUserPasswordIfAuthVersion(ctx, user.ID, user.AuthVersion, passwordHash, false)
			errs <- err
		}(hash)
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent password mutation error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("password CAS successes=%d conflicts=%d want 1/1", successes, conflicts)
	}
}

func TestAuthPhase2ConcurrentEnableAndPasswordAssignmentCannotLeavePendingPasswordUser(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	disabledStore, ok := any(s).(disabledUserMutator)
	if !ok {
		t.Fatal("postgres auth store does not implement atomic enable/disable mutation")
	}

	for i := 0; i < 20; i++ {
		user := authUser(fmt.Sprintf("enable-race-%d", i), fmt.Sprintf("enable-race-%d@example.com", i), store.UserStatusDisabled)
		user.PasswordHash = ""
		user, err := s.CreateUser(ctx, user)
		if err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.SetUserPassword(ctx, user.ID, "assigned-hash", true)
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := disabledStore.SetUserDisabled(ctx, user.ID, false)
			errs <- err
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("iteration %d concurrent enable/password error: %v", i, err)
			}
		}

		stored, err := s.GetUser(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.PasswordHash != "" && stored.Status == store.UserStatusPending {
			t.Fatalf("iteration %d invalid final lifecycle state: status=%q password hash is populated", i, stored.Status)
		}
		if stored.PasswordHash != "assigned-hash" || stored.Status != store.UserStatusActive {
			t.Fatalf("iteration %d final user status=%q password=%q want active/assigned-hash", i, stored.Status, stored.PasswordHash)
		}
	}
}

func TestAuthPhase2AtomicEnableDerivesStatusFromCurrentPasswordState(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	disabledStore, ok := any(s).(disabledUserMutator)
	if !ok {
		t.Fatal("postgres auth store does not implement atomic enable/disable mutation")
	}

	passwordless := authUser("passwordless-disabled", "passwordless-disabled@example.com", store.UserStatusDisabled)
	passwordless, err := s.CreateUser(ctx, passwordless)
	if err != nil {
		t.Fatal(err)
	}
	reenabled, err := disabledStore.SetUserDisabled(ctx, passwordless.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if reenabled.Status != store.UserStatusPending {
		t.Fatalf("passwordless enable status=%q want pending", reenabled.Status)
	}

	withPassword := authUser("password-disabled", "password-disabled@example.com", store.UserStatusDisabled)
	withPassword.PasswordHash = "hash"
	withPassword, err = s.CreateUser(ctx, withPassword)
	if err != nil {
		t.Fatal(err)
	}
	reenabled, err = disabledStore.SetUserDisabled(ctx, withPassword.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if reenabled.Status != store.UserStatusActive {
		t.Fatalf("password-bearing enable status=%q want active", reenabled.Status)
	}
}
