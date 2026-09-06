package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type cancellationEventStore struct {
	processTestStore
}

func (s *cancellationEventStore) AppendEvent(ctx context.Context, event store.Event) (store.Event, error) {
	if err := ctx.Err(); err != nil {
		return store.Event{}, err
	}
	return s.processTestStore.AppendEvent(ctx, event)
}

func TestProcessLauncherPersistsTerminalEventAfterCancellation(t *testing.T) {
	safe := processTestSafeContext(t.TempDir())
	evidenceStore := &cancellationEventStore{}
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
	client := newLauncherClient("", "", 1, errors.New("transport interrupted"))
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
	process, err := launcher.Start(t.Context(), engine.ProcessRequest{Kind: "tool", Name: "cancelled", Command: []string{"fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := process.Wait(ctx); err == nil {
		t.Fatal("Wait() unexpectedly succeeded after cancellation")
	}
	if !hasProcessTestEvent(evidenceStore.events, "tool.failed") {
		t.Fatalf("terminal failure event missing after cancellation: %+v", evidenceStore.events)
	}
}
