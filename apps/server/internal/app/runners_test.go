package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"strings"
	"testing"
	"time"
)

type runnerMemory struct {
	store.RunnerStore
	value store.Runner
}

func (m *runnerMemory) CreateRunner(_ context.Context, r store.Runner) (store.Runner, error) {
	r.ID = "runner-id"
	m.value = r
	return r, nil
}
func (m *runnerMemory) GetRunner(_ context.Context, id string) (store.Runner, error) {
	if id != m.value.ID {
		return store.Runner{}, store.ErrNotFound
	}
	return m.value, nil
}
func (m *runnerMemory) RotateRunnerCredential(_ context.Context, id string, hash []byte) (store.Runner, error) {
	m.value.TokenHash = hash
	return m.value, nil
}
func (m *runnerMemory) RevokeRunner(_ context.Context, id string, deleted bool) (store.Runner, error) {
	if id != m.value.ID {
		return store.Runner{}, store.ErrNotFound
	}
	now := time.Now()
	m.value.RevokedAt = &now
	if deleted {
		m.value.DeletedAt = &now
	}
	return m.value, nil
}

type runnerSessionTerminatorFake struct {
	runnerID string
}

func (f *runnerSessionTerminatorFake) TerminateRunnerSessions(_ context.Context, runnerID string) error {
	f.runnerID = runnerID
	return nil
}

func TestRunnerCredentialsAreOneTimeHashedAndValidated(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	s := NewRunnerService(memory)
	r, token, err := s.Create(ctx, "Build host")
	if err != nil || token == "" {
		t.Fatalf("create: %v", err)
	}
	hash := sha256.Sum256([]byte(token))
	if string(memory.value.TokenHash) != string(hash[:]) {
		t.Fatal("plaintext or invalid hash persisted")
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "TokenHash") {
		t.Fatal("credential exposed")
	}
	if _, err = s.Authenticate(ctx, r.ID, token); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ id, token string }{{r.ID, "wrong"}, {"wrong", token}, {r.ID, ""}} {
		if _, err = s.Authenticate(ctx, input.id, input.token); !errors.Is(err, ErrRunnerAuthentication) {
			t.Fatalf("authentication: %v", err)
		}
	}
	_, next, err := s.Rotate(ctx, r.ID)
	if err != nil || next == token {
		t.Fatalf("rotate: %v", err)
	}
	if _, err = s.Authenticate(ctx, r.ID, token); !errors.Is(err, ErrRunnerAuthentication) {
		t.Fatal("previous token valid")
	}
	if _, err = s.Authenticate(ctx, r.ID, next); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	memory.value.RevokedAt = &now
	if _, err = s.Authenticate(ctx, r.ID, next); !errors.Is(err, ErrRunnerAuthentication) {
		t.Fatal("revoked accepted")
	}
	memory.value.RevokedAt = nil
	memory.value.DeletedAt = &now
	if _, err = s.Authenticate(ctx, r.ID, next); !errors.Is(err, ErrRunnerAuthentication) {
		t.Fatal("deleted accepted")
	}
}

func TestRevokeTerminatesActiveRunnerSessions(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{value: store.Runner{ID: "runner-1"}}
	terminator := &runnerSessionTerminatorFake{}
	service := NewRunnerService(memory)
	service.SetSessionTerminator(terminator)
	if _, err := service.Revoke(ctx, "runner-1", false); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if terminator.runnerID != "runner-1" {
		t.Fatalf("terminator runner=%q", terminator.runnerID)
	}
}

func TestRunnerManagedCredentialNotUserAccessible(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	s := NewRunnerService(memory)
	for _, name := range []string{"", "  "} {
		if _, _, err := s.Create(ctx, name); err == nil {
			t.Fatal("empty name accepted")
		}
	}
	memory.value = store.Runner{ID: "internal", Internal: true}
	if _, token, err := s.Rotate(ctx, "internal"); err == nil || token != "" {
		t.Fatal("internal token exposed")
	}
}
