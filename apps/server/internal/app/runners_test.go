package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	runnerconn "github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runnerMemory struct {
	store.RunnerStore
	value        store.Runner
	reservations map[string]int
}

func (m *runnerMemory) CreateRunner(_ context.Context, r store.Runner) (store.Runner, error) {
	r.ID = "runner-id"
	now := time.Now().UTC()
	if len(r.TokenHash) != 0 {
		r.RegisteredAt = &now
	}
	m.value = r
	return r, nil
}
func (m *runnerMemory) RegisterRunner(_ context.Context, hash []byte, r store.Runner) (store.Runner, error) {
	if m.value.ID == "" || m.value.RegisteredAt != nil || m.value.Internal || m.value.RevokedAt != nil || m.value.DeletedAt != nil || string(m.value.RegistrationTokenHash) != string(hash) {
		return store.Runner{}, store.ErrNotFound
	}
	now := time.Now().UTC()
	m.value.Name = r.Name
	m.value.TokenHash = append([]byte(nil), r.TokenHash...)
	m.value.RegistrationTokenHash = nil
	m.value.RegisteredAt = &now
	return m.value, nil
}
func (m *runnerMemory) GetRunner(_ context.Context, id string) (store.Runner, error) {
	if id != m.value.ID || m.value.DeletedAt != nil {
		return store.Runner{}, store.ErrNotFound
	}
	return m.value, nil
}
func (m *runnerMemory) RotateRunnerCredential(_ context.Context, id string, hash []byte) (store.Runner, error) {
	if id != m.value.ID || m.value.RegisteredAt == nil || m.value.RevokedAt != nil || m.value.DeletedAt != nil {
		return store.Runner{}, store.ErrNotFound
	}
	m.value.TokenHash = hash
	return m.value, nil
}
func (m *runnerMemory) ListProjectRunnerIDs(context.Context, string) ([]string, error) {
	if m.value.ID == "" {
		return nil, nil
	}
	return []string{m.value.ID}, nil
}
func (m *runnerMemory) SetProjectRunnerIDs(context.Context, string, []string) error { return nil }
func (m *runnerMemory) RenameRunner(ctx context.Context, id, name string) (store.Runner, error) {
	return m.UpdateRunner(ctx, id, name, nil)
}
func (m *runnerMemory) UpdateRunner(_ context.Context, id, name string, maxActiveSessions *int) (store.Runner, error) {
	if id != m.value.ID || m.value.RegisteredAt == nil || m.value.Internal {
		return store.Runner{}, store.ErrNotFound
	}
	m.value.Name = name
	if maxActiveSessions != nil {
		m.value.Capabilities = runnerconn.WithConfiguredMaxActiveSessions(m.value.Capabilities, *maxActiveSessions)
	}
	return m.value, nil
}
func (m *runnerMemory) CountRunnerReservations(_ context.Context, ids []string) (map[string]int, error) {
	if len(ids) == 0 {
		return map[string]int{}, nil
	}
	counts := map[string]int{}
	for _, id := range ids {
		if count, ok := m.reservations[id]; ok {
			counts[id] = count
		}
	}
	return counts, nil
}
func (m *runnerMemory) RevokeRunner(_ context.Context, id string, deleted bool) (store.Runner, error) {
	if id != m.value.ID || m.value.Internal || m.value.DeletedAt != nil {
		return store.Runner{}, store.ErrNotFound
	}
	now := time.Now().UTC()
	m.value.RevokedAt = &now
	m.value.RegistrationTokenHash = nil
	if deleted {
		m.value.DeletedAt = &now
	}
	return m.value, nil
}

type runnerSessionTerminatorFake struct{ runnerID string }

func (f *runnerSessionTerminatorFake) TerminateRunnerSessions(_ context.Context, runnerID string) error {
	f.runnerID = runnerID
	return nil
}

func TestRunnerRegistrationCreatesPendingIdentityThenRegistersSameRunner(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	s := NewRunnerService(memory)

	pending, registrationToken, err := s.Create(ctx)
	if err != nil || registrationToken == "" {
		t.Fatalf("create: runner=%#v token=%q err=%v", pending, registrationToken, err)
	}
	if pending.ID == "" || pending.Name != "" || pending.RegisteredAt != nil || len(pending.TokenHash) != 0 {
		t.Fatalf("runner was not created pending and unnamed: %#v", pending)
	}
	registrationHash := sha256.Sum256([]byte(registrationToken))
	if string(memory.value.RegistrationTokenHash) != string(registrationHash[:]) || string(memory.value.RegistrationTokenHash) == registrationToken {
		t.Fatal("registration token was not stored only as a hash on the pending runner")
	}
	if _, err := s.Authenticate(ctx, pending.ID, registrationToken); !errors.Is(err, ErrRunnerAuthentication) {
		t.Fatalf("registration token authenticated pending runner: %v", err)
	}

	registered, runnerToken, err := s.Register(ctx, registrationToken, " build-host ")
	if err != nil || runnerToken == "" {
		t.Fatalf("register: %v", err)
	}
	if registered.ID != pending.ID || registered.Name != "build-host" || registered.RegisteredAt == nil {
		t.Fatalf("registration did not update the same runner: pending=%#v registered=%#v", pending, registered)
	}
	if len(memory.value.RegistrationTokenHash) != 0 {
		t.Fatal("registration token hash was not consumed")
	}
	credentialHash := sha256.Sum256([]byte(runnerToken))
	if string(memory.value.TokenHash) != string(credentialHash[:]) {
		t.Fatal("plaintext or invalid runner credential persisted")
	}
	if _, _, err := s.Register(ctx, registrationToken, "second-host"); err == nil {
		t.Fatal("registration token reused")
	}
	encoded, _ := json.Marshal(registered)
	if strings.Contains(string(encoded), runnerToken) || strings.Contains(string(encoded), "TokenHash") || strings.Contains(string(encoded), registrationToken) {
		t.Fatal("credential exposed")
	}
	if _, err = s.Authenticate(ctx, registered.ID, runnerToken); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCredentialRotationRevocationAndDeletion(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	s := NewRunnerService(memory)
	pending, registrationToken, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, token, err := s.Rotate(ctx, pending.ID); err == nil || token != "" {
		t.Fatal("pending runner received a permanent credential")
	}
	registered, token, err := s.Register(ctx, registrationToken, "Build host")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ id, token string }{{registered.ID, "wrong"}, {"wrong", token}, {registered.ID, ""}} {
		if _, err = s.Authenticate(ctx, input.id, input.token); !errors.Is(err, ErrRunnerAuthentication) {
			t.Fatalf("authentication: %v", err)
		}
	}
	_, next, err := s.Rotate(ctx, registered.ID)
	if err != nil || next == token {
		t.Fatalf("rotate: %v", err)
	}
	if _, err = s.Authenticate(ctx, registered.ID, token); !errors.Is(err, ErrRunnerAuthentication) {
		t.Fatal("previous token valid")
	}
	if _, err = s.Authenticate(ctx, registered.ID, next); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Revoke(ctx, registered.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, registered.ID, next); !errors.Is(err, ErrRunnerAuthentication) {
		t.Fatal("revoked accepted")
	}
	if _, err = s.Revoke(ctx, registered.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, registered.ID, next); !errors.Is(err, ErrRunnerAuthentication) {
		t.Fatal("deleted accepted")
	}
}

func TestPendingRunnerRevokeAndDeleteInvalidateRegistration(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoke", true: "delete"}[deleted], func(t *testing.T) {
			ctx := context.Background()
			memory := &runnerMemory{}
			s := NewRunnerService(memory)
			pending, registrationToken, err := s.Create(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Revoke(ctx, pending.ID, deleted); err != nil {
				t.Fatal(err)
			}
			if len(memory.value.RegistrationTokenHash) != 0 {
				t.Fatal("pending registration hash survived revoke/delete")
			}
			if _, _, err := s.Register(ctx, registrationToken, "host"); err == nil {
				t.Fatal("revoked/deleted pending runner registered")
			}
		})
	}
}

func TestRunnerServiceCountReservations(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{reservations: map[string]int{"runner-id": 2}}
	s := NewRunnerService(memory)
	counts, err := s.CountReservations(ctx, []string{"runner-id", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["runner-id"] != 2 || counts["missing"] != 0 {
		t.Fatalf("counts=%v", counts)
	}
	empty, err := s.CountReservations(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty counts=%v err=%v", empty, err)
	}
}

func TestRunnerServiceUpdateMaxActiveSessions(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	s := NewRunnerService(memory)
	_, registrationToken, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := s.Register(ctx, registrationToken, "Build host")
	if err != nil {
		t.Fatal(err)
	}
	capacity := 4
	updated, err := s.Update(ctx, created.ID, created.Name, &capacity)
	if err != nil {
		t.Fatal(err)
	}
	if runnerconn.MaxActiveSessions(updated.Capabilities) != 4 {
		t.Fatalf("updated=%s", updated.Capabilities)
	}
	if _, err := s.Update(ctx, created.ID, created.Name, intPtr(0)); err == nil {
		t.Fatal("zero capacity accepted")
	}
}

func intPtr(v int) *int { return &v }

func TestRunnerServiceListsRenamesAndScopesProjectRunners(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	s := NewRunnerService(memory)
	pending, registrationToken, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, pending.ID, "Too early"); err == nil {
		t.Fatal("pending runner renamed")
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
	now := time.Now().UTC()
	memory := &runnerMemory{value: store.Runner{ID: "runner-1", RegisteredAt: &now}}
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
	for _, input := range []struct{ token, hostname string }{{"", "host"}, {"token", ""}, {"token", "  "}} {
		if _, _, err := s.Register(ctx, input.token, input.hostname); err == nil {
			t.Fatal("invalid registration accepted")
		}
	}
	now := time.Now().UTC()
	memory.value = store.Runner{ID: "internal", Name: "Internal", Internal: true, RegisteredAt: &now, TokenHash: make([]byte, 32)}
	if _, token, err := s.Rotate(ctx, "internal"); err == nil || token != "" {
		t.Fatal("internal token exposed")
	}
}
