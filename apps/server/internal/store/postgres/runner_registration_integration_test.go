package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunnerRegistrationUpdatesPendingRunnerAndRollsBackNameConflict(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	credentialHash := make([]byte, 32)
	credentialHash[0] = 1
	if _, err := s.CreateRunner(ctx, store.Runner{Name: "Taken host", TokenHash: credentialHash}); err != nil {
		t.Fatal(err)
	}

	registrationHash := make([]byte, 32)
	registrationHash[0] = 2
	pending, err := s.CreateRunner(ctx, store.Runner{RegistrationTokenHash: registrationHash})
	if err != nil {
		t.Fatal(err)
	}
	if pending.ID == "" || pending.Name != "" || pending.RegisteredAt != nil || len(pending.TokenHash) != 0 || string(pending.RegistrationTokenHash) != string(registrationHash) {
		t.Fatalf("invalid pending runner %#v", pending)
	}

	if _, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "Taken host", TokenHash: credentialHash}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected conflicting runner name, got %v", err)
	}

	registered, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "Available host", TokenHash: credentialHash})
	if err != nil {
		t.Fatalf("registration token was consumed by rolled-back update: %v", err)
	}
	if registered.ID != pending.ID || registered.Name != "Available host" || registered.Internal || registered.RegisteredAt == nil || len(registered.RegistrationTokenHash) != 0 {
		t.Fatalf("unexpected registered runner %#v", registered)
	}
	if _, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "Another host", TokenHash: credentialHash}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("used registration token accepted again: %v", err)
	}
}

func TestRunnerRegistrationConcurrentTokenConsumption(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	registrationHash := make([]byte, 32)
	registrationHash[0] = 3
	pending, err := s.CreateRunner(ctx, store.Runner{RegistrationTokenHash: registrationHash})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	type result struct {
		runner store.Runner
		err    error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			hash := make([]byte, 32)
			hash[0] = byte(10 + index)
			runner, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "Concurrent host", TokenHash: hash})
			results <- result{runner: runner, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for result := range results {
		if result.err == nil {
			successes++
			if result.runner.ID != pending.ID || result.runner.RegisteredAt == nil {
				t.Fatalf("winner registered wrong runner %#v", result.runner)
			}
			continue
		}
		if !errors.Is(result.err, store.ErrNotFound) {
			t.Fatalf("concurrent registration error=%v", result.err)
		}
		failures++
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("successes=%d failures=%d", successes, failures)
	}
	registered, err := s.GetRunner(ctx, pending.ID)
	if err != nil || registered.RegisteredAt == nil || len(registered.TokenHash) != 32 || len(registered.RegistrationTokenHash) != 0 {
		t.Fatalf("resulting runner=%#v err=%v", registered, err)
	}
}

func TestRevokedOrDeletedPendingRunnerCannotRegister(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked", true: "deleted"}[deleted], func(t *testing.T) {
			s := New(testPool(t))
			ctx := context.Background()
			registrationHash := make([]byte, 32)
			registrationHash[0] = 4
			pending, err := s.CreateRunner(ctx, store.Runner{RegistrationTokenHash: registrationHash})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.RevokeRunner(ctx, pending.ID, deleted); err != nil {
				t.Fatal(err)
			}
			credentialHash := make([]byte, 32)
			credentialHash[0] = 5
			if _, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "host", TokenHash: credentialHash}); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("pending runner registered after revoke/delete: %v", err)
			}
		})
	}
}
