package runexec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

type uncertaintyExecutionStore struct {
	*runnerSyncStore
	*launcherSessionStore
}

func (s *uncertaintyExecutionStore) ListProjects(context.Context) ([]store.Project, error) {
	return []store.Project{{ID: s.launcherSessionStore.run.ProjectID}}, nil
}

func (s *uncertaintyExecutionStore) ListExecutionSessions(_ context.Context, projectID string, statuses []string) ([]store.ExecutionSession, error) {
	s.launcherSessionStore.mu.Lock()
	defer s.launcherSessionStore.mu.Unlock()
	values := make([]store.ExecutionSession, 0, len(s.launcherSessionStore.sessions))
	for _, session := range s.launcherSessionStore.sessions {
		if session.ProjectID != projectID {
			continue
		}
		if len(statuses) > 0 {
			matched := false
			for _, status := range statuses {
				if session.Status == status {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		values = append(values, session)
	}
	return values, nil
}

type disconnectingRunnerEngine struct{}

func (disconnectingRunnerEngine) Name() string { return "disconnecting-runner" }

func (disconnectingRunnerEngine) Execute(ctx context.Context, request engine.Request) (engine.Result, error) {
	process, err := request.Launcher.Start(ctx, engine.ProcessRequest{
		Kind: "tool", Name: "disconnect", Command: []string{"agent"},
	})
	if err != nil {
		return engine.Result{}, err
	}
	_, err = process.Wait(ctx)
	return engine.Result{}, err
}

func TestProcessorProcessPreservesRunnerDisconnectUncertainty(t *testing.T) {
	repository := initProcessTestRepository(t)
	safe := processTestSafeContext(repository)
	safe.Runtime = executioncontext.RuntimeContext{}
	safe.Agent.Engine = "disconnecting-runner"
	run := store.Run{
		ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID,
		WorkspaceID: safe.Workspace.ID, AgentID: &safe.Agent.ID, Status: "STARTING",
	}

	base := &runnerSyncStore{}
	sessionStore := &launcherSessionStore{run: run}
	storeFake := &uncertaintyExecutionStore{runnerSyncStore: base, launcherSessionStore: sessionStore}
	transport := newLauncherClient("", "", 0, runner.ErrDisconnected)
	executionSessions, err := newLauncherExecutionSessionService(storeFake, transport)
	if err != nil {
		t.Fatal(err)
	}
	authorizedSessions, err := app.NewAuthorizedExecutionSessionService(executionSessions, launcherPreparer{})
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(storeFake, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(storeFake, blobs, 64)
	if err != nil {
		t.Fatal(err)
	}
	engines, err := engine.NewRegistry(disconnectingRunnerEngine{})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	syncClient := &successfulSyncClient{payload: runnerTransferPayload(t, repository)}
	processor, err := NewProcessor(
		storeFake,
		processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
		nil,
		authorizedSessions,
		engines,
		recorder,
		output,
		git,
		runnerSyncConnector{client: syncClient},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{
		RunnerID: "runner-1",
		Job:      store.SchedulerJob{ID: "job-1", ProjectID: run.ProjectID, RunID: run.ID, Kind: "START", State: "CLAIMED"},
		Lease:    store.SchedulerLease{JobID: "job-1", LeaseToken: "lease-1"},
		Run:      run,
	}, processTestLifecycle{run: run})
	if err == nil || !errors.Is(err, runner.ErrDisconnected) {
		t.Fatalf("Process error=%v want runner disconnect uncertainty", err)
	}
	if result.RunStatus != "" {
		t.Fatalf("Process result=%+v want no terminal Run result", result)
	}

	sessions, err := storeFake.ListExecutionSessions(t.Context(), run.ProjectID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Status != "RUNNING" {
		t.Fatalf("Execution Sessions=%+v want one authoritative RUNNING session", sessions)
	}
	if hasProcessTestEvent(base.events, "run.failed") {
		t.Fatalf("uncertain runner disconnect emitted terminal run.failed evidence: %+v", base.events)
	}

	transport.waitErr = nil
	if err := authorizedSessions.ReconcileAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sessions, err = storeFake.ListExecutionSessions(t.Context(), run.ProjectID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) == 1 && sessions[0].Status == "COMPLETED" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Execution Session did not reconcile to a trustworthy terminal state: %+v", sessions)
}
