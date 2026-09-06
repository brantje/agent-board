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
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type capturingRequestSessions struct {
	request app.AuthorizedExecutionRequest
	err     error
}

func (s *capturingRequestSessions) Start(_ context.Context, _, _, _ string, request app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	s.request = request
	return nil, s.err
}

func (*capturingRequestSessions) ReconcileAll(context.Context) error { return nil }

func TestProcessLauncherPreservesCredentialAndSecretSelectors(t *testing.T) {
	safe := processTestSafeContext(t.TempDir())
	evidenceStore := &processTestStore{}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &capturingRequestSessions{err: errors.New("stop after capture")}
	launcher := &processLauncher{
		sessions:          sessions,
		events:            recorder,
		safe:              safe,
		runtimeInstanceID: "runtime-instance",
		scope:             evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
	}
	refs := map[string]string{"SERVICE_TOKEN": "project/service-token"}
	_, err = launcher.Start(t.Context(), engine.ProcessRequest{
		Kind:                  "tool",
		Name:                  "credentialed",
		Command:               []string{"fixture"},
		ProviderCredentialEnv: "PROVIDER_API_KEY",
		RuntimeSecretRefs:     refs,
	})
	if err == nil {
		t.Fatal("expected captured start error")
	}
	if sessions.request.ProviderCredentialEnv != "PROVIDER_API_KEY" {
		t.Fatalf("provider credential env=%q", sessions.request.ProviderCredentialEnv)
	}
	if got := sessions.request.RuntimeSecretRefs["SERVICE_TOKEN"]; got != "project/service-token" {
		t.Fatalf("runtime secret ref=%q", got)
	}
	refs["SERVICE_TOKEN"] = "mutated"
	if got := sessions.request.RuntimeSecretRefs["SERVICE_TOKEN"]; got != "project/service-token" {
		t.Fatalf("runtime secret refs were not copied: %q", got)
	}
}

func TestProcessLauncherWaitDoesNotRequireStreamConsumers(t *testing.T) {
	safe := processTestSafeContext(t.TempDir())
	evidenceStore := &processTestStore{}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(evidenceStore, blobs, 16)
	if err != nil {
		t.Fatal(err)
	}
	sessionStore := &launcherSessionStore{
		run:      store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, WorkspaceID: safe.Workspace.ID},
		instance: store.RuntimeInstance{ID: "runtime-instance", ProjectID: safe.Project.ID, WorkspaceID: safe.Workspace.ID, RuntimeID: safe.Runtime.ID, Status: "RUNNING"},
	}
	client := newLauncherClient(strings.Repeat("stdout-", 128), strings.Repeat("stderr-", 128), 0, nil)
	transportSessions, err := app.NewExecutionSessionService(sessionStore, launcherRunnerManager{client: client})
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := app.NewAuthorizedExecutionSessionService(transportSessions, launcherPreparer{runtimeID: safe.Runtime.ID})
	if err != nil {
		t.Fatal(err)
	}
	launcher := &processLauncher{
		sessions:          authorized,
		events:            recorder,
		output:            output,
		safe:              safe,
		runtimeInstanceID: "runtime-instance",
		scope:             evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
	}
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
