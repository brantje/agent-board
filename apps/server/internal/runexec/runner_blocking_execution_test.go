package runexec

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type blockingRunnerEngine struct{}

func (blockingRunnerEngine) Name() string { return "blocking-runner" }
func (blockingRunnerEngine) Execute(context.Context, engine.Request) (engine.Result, error) {
	return engine.Result{}, engine.ErrWaitingForInput
}

func TestRunEngineOnRunnerKeepsWorkspaceOwnedWhileWaitingForInput(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Agent.Engine = "blocking-runner"

	eventStore := &runnerSyncStore{}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, repo)}
	processor := newRunnerSyncProcessor(t, repo, safe, eventStore, client)
	registry, err := engine.NewRegistry(blockingRunnerEngine{})
	if err != nil {
		t.Fatal(err)
	}
	processor.engines = registry
	processor.store = &runnerQuestionStore{
		orchestrationQuestionStore: &orchestrationQuestionStore{processTestStore: &processTestStore{}},
		open: store.Question{
			ID:        "question-1",
			ProjectID: safe.Project.ID,
			IssueID:   safe.Issue.ID,
			RunID:     safe.Run.ID,
			Blocking:  true,
			Status:    "OPEN",
		},
	}

	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}
	result, err := processor.runEngineOnRunner(t.Context(), run, safe, "runner-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "WAITING_FOR_INPUT" {
		t.Fatalf("result=%+v", result)
	}
	if len(client.directions) != 0 || client.confirmed {
		t.Fatalf("blocked Runner workspace was synced prematurely: directions=%v confirmed=%v", client.directions, client.confirmed)
	}
}
