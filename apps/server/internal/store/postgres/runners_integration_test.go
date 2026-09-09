package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunnerPersistenceLifecycle(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	r, err := s.CreateRunner(ctx, store.Runner{Name: "Build host", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.Internal || r.DeletedAt != nil {
		t.Fatal("invalid created runner")
	}
	if _, err := s.CreateRunner(ctx, store.Runner{Name: "build HOST", TokenHash: make([]byte, 32)}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	r, err = s.RenameRunner(ctx, r.ID, "Renamed")
	if err != nil || r.Name != "Renamed" {
		t.Fatalf("rename: %v", err)
	}
	hash := make([]byte, 32)
	hash[0] = 1
	if _, err = s.RotateRunnerCredential(ctx, r.ID, hash); err != nil {
		t.Fatal(err)
	}
	r, err = s.GetRunner(ctx, r.ID)
	if err != nil || r.TokenHash[0] != 1 {
		t.Fatalf("rotation: %v", err)
	}
	if _, err = s.RevokeRunner(ctx, r.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RotateRunnerCredential(ctx, r.ID, hash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked credential restored: %v", err)
	}
	if _, err = s.RevokeRunner(ctx, r.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetRunner(ctx, r.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted runner visible: %v", err)
	}
	if _, err = s.CreateRunner(ctx, store.Runner{Name: "Renamed", TokenHash: hash}); err != nil {
		t.Fatalf("name reuse: %v", err)
	}
	values, err := s.ListRunners(ctx)
	if err != nil || len(values) != 1 {
		t.Fatalf("active listing: %d %v", len(values), err)
	}
}

func TestInternalRunnerIsUniqueAndProtected(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	r, err := s.CreateRunner(ctx, store.Runner{Name: "Internal", Internal: true, TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRunner(ctx, store.Runner{Name: "Second", Internal: true, TokenHash: make([]byte, 32)}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second internal: %v", err)
	}
	for _, deleted := range []bool{false, true} {
		if _, err = s.RevokeRunner(ctx, r.ID, deleted); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("managed lifecycle: %v", err)
		}
	}
	if _, err = s.RenameRunner(ctx, r.ID, "User rename"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("managed rename: %v", err)
	}
}
