package runexec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
)

type capturingRequestSessions struct {
	request app.AuthorizedExecutionRequest
	err     error
}

func (s *capturingRequestSessions) StartOnRunner(_ context.Context, _, _, _ string, request app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	s.request = request
	return nil, s.err
}

func (s *capturingRequestSessions) StartPreparedOnRunner(_ context.Context, _, _ string, request app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	s.request = request
	return nil, s.err
}

func (*capturingRequestSessions) Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error) {
	return nil, errors.New("unexpected process attach")
}

func (*capturingRequestSessions) ReconcileAll(context.Context) error { return nil }

func TestProcessLauncherPreservesCredentialAndEnvironmentSelectors(t *testing.T) {
	safe := processTestSafeContext(t.TempDir())
	evidenceStore := &processTestStore{}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &capturingRequestSessions{err: errors.New("stop after capture")}
	launcher := &processLauncher{
		sessions: sessions,
		events:   recorder,
		safe:     safe,
		runnerID: "runner-1",
		scope:    evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
	}
	env := map[string]string{"MODE": "test"}
	_, err = launcher.Start(t.Context(), engine.ProcessRequest{
		Kind:                  "tool",
		Name:                  "credentialed",
		Command:               []string{"fixture"},
		Env:                   env,
		ProviderCredentialEnv: "PROVIDER_API_KEY",
	})
	if err == nil {
		t.Fatal("expected captured start error")
	}
	if sessions.request.ProviderCredentialEnv != "PROVIDER_API_KEY" {
		t.Fatalf("provider credential env=%q", sessions.request.ProviderCredentialEnv)
	}
	if got := sessions.request.Env["MODE"]; got != "test" {
		t.Fatalf("environment=%q", got)
	}
	env["MODE"] = "mutated"
	if got := sessions.request.Env["MODE"]; got != "test" {
		t.Fatalf("environment was not copied: %q", got)
	}
}

func TestProcessLauncherWaitDoesNotRequireStreamConsumers(t *testing.T) {
	safe := processTestSafeContext(t.TempDir())
	evidenceStore := &processTestStore{}
	client := newLauncherClient(strings.Repeat("stdout-", 128), strings.Repeat("stderr-", 128), 0, nil)
	launcher := newLauncher(t, safe, evidenceStore, client)
	process, err := launcher.Start(t.Context(), engine.ProcessRequest{Kind: "tool", Name: "no-stream-consumer", Command: []string{"fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := process.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code=%d", result.ExitCode)
	}
	if len(evidenceStore.chunks) < 2 {
		t.Fatalf("raw output chunks=%d, want complete stdout/stderr evidence", len(evidenceStore.chunks))
	}
}
