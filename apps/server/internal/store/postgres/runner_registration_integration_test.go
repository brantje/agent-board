package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunnerRegistrationConsumesTokenOnlyWithSuccessfulCreate(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	credentialHash := make([]byte, 32)
	credentialHash[0] = 1
	if _, err := s.CreateRunner(ctx, store.Runner{Name: "Taken host", TokenHash: credentialHash}); err != nil {
		t.Fatal(err)
	}

	registrationHash := make([]byte, 32)
	registrationHash[0] = 2
	if err := s.CreateRunnerRegistration(ctx, registrationHash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "Taken host", TokenHash: credentialHash}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected conflicting runner name, got %v", err)
	}

	registered, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "Available host", TokenHash: credentialHash})
	if err != nil {
		t.Fatalf("registration token was consumed by rolled-back create: %v", err)
	}
	if registered.ID == "" || registered.Name != "Available host" || registered.Internal {
		t.Fatalf("unexpected registered runner %#v", registered)
	}
	if _, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "Another host", TokenHash: credentialHash}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("used registration token accepted again: %v", err)
	}
}
