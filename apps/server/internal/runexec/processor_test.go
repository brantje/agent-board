package runexec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reconcileStore struct {
	sessions []store.ExecutionSession
}

func (*reconcileStore) PutRunProvenance(context.Context, string, string, json.RawMessage) error {
	return nil
}

func (*reconcileStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) {
	return nil, store.ErrNotFound
}

func (s *reconcileStore) ListExecutionSessions(context.Context, string, []string) ([]store.ExecutionSession, error) {
	return append([]store.ExecutionSession(nil), s.sessions...), nil
}

func (*reconcileStore) GetWorkspace(context.Context, string, string) (store.Workspace, error) {
	return store.Workspace{}, store.ErrNotFound
}

func (*reconcileStore) UpdateWorkspaceCurrentBranch(context.Context, string, string, string) (store.Workspace, error) {
	return store.Workspace{}, store.ErrNotFound
}

type reconcileSessions struct{}

func (reconcileSessions) Start(context.Context, string, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	return nil, nil
}
func (reconcileSessions) Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error) {
	return nil, nil
}
func (reconcileSessions) ReconcileAll(context.Context) error { return nil }

func TestReconcileDoesNotBlindlyReplayExistingExecution(t *testing.T) {
	claim := &store.SchedulerAdmission{Run: store.Run{ID: "run", ProjectID: "project"}}
	cases := []struct {
		name     string
		sessions []store.ExecutionSession
		want     store.SchedulerReconciliationOutcome
	}{
		{name: "no execution started", want: store.SchedulerReconciliationRetry},
		{name: "active execution", sessions: []store.ExecutionSession{{RunID: "run", Status: "RUNNING"}}, want: store.SchedulerReconciliationActive},
		{name: "completed execution", sessions: []store.ExecutionSession{{RunID: "run", Status: "COMPLETED"}}, want: store.SchedulerReconciliationUnknown},
		{name: "failed execution", sessions: []store.ExecutionSession{{RunID: "run", Status: "FAILED"}}, want: store.SchedulerReconciliationUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			processor := &Processor{store: &reconcileStore{sessions: tc.sessions}, sessions: reconcileSessions{}}
			got, _, err := processor.Reconcile(context.Background(), claim)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
