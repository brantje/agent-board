package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runnerMemory struct {
	store.RunnerStore
	value            store.Runner
	registrationHash []byte
}

func (m *runnerMemory) CreateRunner(_ context.Context, r store.Runner) (store.Runner, error) {
	r.ID = "runner-id"
	m.value = r
	return r, nil
}
func (m *runnerMemory) CreateRunnerRegistration(_ context.Context, hash []byte) error {
	m.registrationHash = append([]byte(nil), hash...)
	return nil
}
func (m *runnerMemory) RegisterRunner(ctx context.Context, hash []byte, r store.Runner) (store.Runner, error) {
	if len(m.registrationHash) == 0 || string(m.registrationHash) != string(hash) {
		return store.Runner{}, store.ErrNotFound
	}
	m.registrationHash = nil
	return m.CreateRunner(ctx, r)
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
func (m *runnerMemory) ListProjectRunnerIDs(context.Context, string) ([]string, error) {
	if m.value.ID == "" {
		return nil, nil
	}
	return []string{m.value.ID}, nil
}

func (m *runnerMemory) SetProjectRunnerIDs(context.Context, string, []string) error {
	return nil
}

func (m *runnerMemory) RenameRunner(_ context.Context, _, name string) (store.Runner, error) {
	m.value.Name = name
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

func TestRunnerRegistrationIsOneTimeAndCredentialsAreHashed(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	s := NewRunnerService(memory)
	registrationToken, err := s.CreateRegistration(ctx)
	if err != nil || registrationToken == "" {
		t.Fatalf("create registration: token=%q err=%v", registrationToken, err)
	}
	registrationHash := sha256.Sum256([]byte(registrationToken))
	if string(memory.registrationHash) != string(registrationHash[:]) || string(memory.registrationHash) == registrationToken {
		t.Fatal("registration token was not stored as a hash")
	}

	r, token, err := s.Register(ctx, registrationToken, "Build host")
	if err != nil || token == "" {
		t.Fatalf("register: %v", err)
	}
	if _, _, err := s.Register(ctx, registrationToken, "Second host"); err == nil {
		t.Fatal("registration token reused")
	}
	credentialHash := sha256.Sum256([]byte(token))
	if string(memory.value.TokenHash) != string(credentialHash[:]) {
		t.Fatal("plaintext or invalid runner credential persisted")
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

func TestRunnerServiceListsRenamesAndScopesProjectRunners(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	s := NewRunnerService(memory)
	registrationToken, err := s.CreateRegistration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := s.Register(ctx, registrationToken, "Build host")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("list=%v err=%v", listed, err)
	}
	renamed, err := s.Rename(ctx, created.ID, "  CI host  ")
	if err != nil || renamed.Name != "CI host" {
		t.Fatalf("rename=%+v err=%v", renamed, err)
	}
	if _, err := s.Rename(ctx, created.ID, " "); err == nil {
		t.Fatal("blank rename accepted")
	}
	if err := s.SetProjectRunners(ctx, "project-1", []string{created.ID}); err != nil {
		t.Fatal(err)
	}
	ids, err := s.ProjectRunners(ctx, "project-1")
	if err != nil || len(ids) != 1 || ids[0] != created.ID {
		t.Fatalf("project runners=%v err=%v", ids, err)
	}
	got, err := s.Get(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get=%+v err=%v", got, err)
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
	for _, input := range []struct{ token, name string }{{"", "host"}, {"token", ""}, {"token", "  "}} {
		if _, _, err := s.Register(ctx, input.token, input.name); err == nil {
			t.Fatal("invalid registration accepted")
		}
	}
	memory.value = store.Runner{ID: "internal", Internal: true}
	if _, token, err := s.Rotate(ctx, "internal"); err == nil || token != "" {
		t.Fatal("internal token exposed")
	}
}
