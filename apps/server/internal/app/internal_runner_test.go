package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (m *runnerMemory) ListRunners(context.Context) ([]store.Runner, error) {
	if m.value.ID == "" {
		return nil, nil
	}
	return []store.Runner{m.value}, nil
}

func TestInternalSupervisionRestartsAndStopsWithoutControlPlaneSecrets(t *testing.T) {
	t.Setenv("AGENT_BOARD_DATABASE_URL", "must-not-reach-child")
	t.Setenv("AGENT_BOARD_SECRET_WRITE_TOKEN", "must-not-reach-child")
	env := strings.Join(internalRunnerEnvironment("http://server", "id", "test-token", "/workspaces"), "\n")
	if strings.Contains(env, "must-not-reach-child") {
		t.Fatal("control-plane secret inherited")
	}
	root := t.TempDir()
	marker := filepath.Join(root, "starts")
	binary := filepath.Join(root, "runner")
	script := "#!/bin/sh\nprintf 'start\\n' >> '" + marker + "'\nexit 0\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	service := NewRunnerService(&runnerMemory{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.SuperviseInternalRunner(ctx, binary, "http://localhost:1", root) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := os.ReadFile(marker)
		if strings.Count(string(data), "start") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child not restarted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop")
	}
	if err := service.SuperviseInternalRunner(context.Background(), filepath.Join(root, "missing"), "http://server", root); !errors.Is(err, ErrInternalRunnerUnavailable) {
		t.Fatalf("missing child: %v", err)
	}
}
func TestInternalCredentialIsGeneratedRotatedAndNotPublic(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	service := NewRunnerService(memory)
	first, token, err := service.prepareInternalRunner(ctx)
	if err != nil || !first.Internal {
		t.Fatalf("managed runner: %v", err)
	}
	hash := sha256.Sum256([]byte(token))
	if string(hash[:]) != string(memory.value.TokenHash) {
		t.Fatal("plaintext persisted")
	}
	second, next, err := service.prepareInternalRunner(ctx)
	if err != nil || second.ID != first.ID || token == next {
		t.Fatalf("managed rotation: %v", err)
	}
	if _, err = service.Authenticate(ctx, first.ID, token); err == nil {
		t.Fatal("old credential works")
	}
	if _, err = service.Authenticate(ctx, first.ID, next); err != nil {
		t.Fatal("managed auth bypass required")
	}
	if _, leaked, err := service.Rotate(ctx, first.ID); err == nil || leaked != "" {
		t.Fatal("managed plaintext exposed")
	}
}
