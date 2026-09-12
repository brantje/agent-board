package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reconcileExecutionStore struct {
	*executionSessionStoreFake
	projects []store.Project
}

func (s *reconcileExecutionStore) ListProjects(context.Context) ([]store.Project, error) {
	return append([]store.Project(nil), s.projects...), nil
}

func (s *reconcileExecutionStore) ListExecutionSessions(_ context.Context, projectID string, statuses []string) ([]store.ExecutionSession, error) {
	if s.session.ID == "" || s.session.ProjectID != projectID {
		return nil, nil
	}
	for _, status := range statuses {
		if s.session.Status == status {
			return []store.ExecutionSession{s.session}, nil
		}
	}
	return nil, nil
}

func TestReconcileLeavesPreparedRunnerSessionPending(t *testing.T) {
	base := &executionSessionStoreFake{
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "PENDING"},
	}
	service, err := NewExecutionSessionService(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.Reconcile(t.Context(), "project-1", "session-1")
	if err != nil || process != nil || base.session.Status != "PENDING" {
		t.Fatalf("prepared runner session process=%v status=%s err=%v", process, base.session.Status, err)
	}
}

func TestReconcileIgnoresTerminalRunnerSession(t *testing.T) {
	base := &executionSessionStoreFake{
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "COMPLETED"},
	}
	service, err := NewExecutionSessionService(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.Reconcile(t.Context(), "project-1", "session-1")
	if err != nil || process != nil || base.session.Status != "COMPLETED" {
		t.Fatalf("terminal runner session process=%v status=%s err=%v", process, base.session.Status, err)
	}
}

func TestReconcileRejectsLiveSessionWithoutRunnerBinding(t *testing.T) {
	base := &executionSessionStoreFake{
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", Status: "RUNNING"},
	}
	service, err := NewExecutionSessionService(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.Reconcile(t.Context(), "project-1", "session-1")
	if err == nil || process != nil || base.session.Status != "RUNNING" {
		t.Fatalf("runner-less live session process=%v status=%s err=%v", process, base.session.Status, err)
	}
}

func TestReconcileAllLeavesPreparedRunnerSessionsForScheduler(t *testing.T) {
	base := &executionSessionStoreFake{
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "PENDING"},
	}
	wrapped := &reconcileExecutionStore{
		executionSessionStoreFake: base,
		projects:                  []store.Project{{ID: "project-1"}},
	}
	service, err := NewExecutionSessionService(wrapped, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ReconcileAll(t.Context()); err != nil {
		t.Fatalf("reconcile all: %v", err)
	}
	if base.session.Status != "PENDING" {
		t.Fatalf("prepared runner session status=%s", base.session.Status)
	}
}
