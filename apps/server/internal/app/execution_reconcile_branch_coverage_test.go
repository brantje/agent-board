package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reconcileBranchStore struct {
	*branchExecutionStore
	projects        []store.Project
	listProjectsErr error
	listSessionsErr error
}

func (s *reconcileBranchStore) ListProjects(context.Context) ([]store.Project, error) {
	if s.listProjectsErr != nil {
		return nil, s.listProjectsErr
	}
	return append([]store.Project(nil), s.projects...), nil
}

func (s *reconcileBranchStore) ListExecutionSessions(_ context.Context, projectID string, statuses []string) ([]store.ExecutionSession, error) {
	if s.listSessionsErr != nil {
		return nil, s.listSessionsErr
	}
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

func reconcileBranchService(t *testing.T, sessionStore ExecutionSessionStore, manager RunnerConnectionManager) *ExecutionSessionService {
	t.Helper()
	service, err := NewExecutionSessionService(sessionStore, manager)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestReconcileAllFailureAndReporterBranches(t *testing.T) {
	plain := newBranchExecutionStore()
	service := reconcileBranchService(t, plain, &reconcileExecutionManager{})
	if err := service.ReconcileAll(context.Background()); err == nil {
		t.Fatal("ReconcileAll() accepted store without list support")
	}

	projectsErr := &reconcileBranchStore{branchExecutionStore: newBranchExecutionStore(), listProjectsErr: store.ErrNotFound}
	service = reconcileBranchService(t, projectsErr, &reconcileExecutionManager{})
	if err := service.ReconcileAll(context.Background()); err == nil {
		t.Fatal("expected ListProjects error")
	}

	listErr := &reconcileBranchStore{
		branchExecutionStore: newBranchExecutionStore(),
		projects: []store.Project{{ID: "project-1"}},
		listSessionsErr: errors.New("list sessions failed"),
	}
	service = reconcileBranchService(t, listErr, &reconcileExecutionManager{})
	if err := service.ReconcileAll(context.Background()); err == nil {
		t.Fatal("expected ListExecutionSessions error")
	}

	reportStore := &reconcileBranchStore{
		branchExecutionStore: newBranchExecutionStore(),
		projects: []store.Project{{ID: "project-1"}},
	}
	reportStore.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"}
	service = reconcileBranchService(t, reportStore, &reconcileExecutionManager{err: runner.ErrDisconnected})
	var reported error
	if err := service.ReconcileAllWithReporter(context.Background(), func(err error) { reported = err }); err != nil {
		t.Fatalf("ReconcileAllWithReporter() error=%v", err)
	}
	if reported == nil || reportStore.session.Status != "RUNNING" {
		t.Fatalf("reported=%v session=%+v", reported, reportStore.session)
	}
}

func TestReconcileDurableStateBranches(t *testing.T) {
	t.Run("terminal no-op", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", Status: "COMPLETED"}
		service := reconcileBranchService(t, storeFake, &reconcileExecutionManager{})
		process, err := service.Reconcile(context.Background(), "project-1", "session-1")
		if err != nil || process != nil {
			t.Fatalf("process=%v err=%v", process, err)
		}
	})

	t.Run("unsupported durable state", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", Status: "BOGUS"}
		service := reconcileBranchService(t, storeFake, &reconcileExecutionManager{})
		if _, err := service.Reconcile(context.Background(), "project-1", "session-1"); err == nil {
			t.Fatal("expected unsupported-state error")
		}
	})

	t.Run("runtime stopped fails session", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.instance.Status = "STOPPED"
		storeFake.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"}
		service := reconcileBranchService(t, storeFake, &reconcileExecutionManager{})
		if _, err := service.Reconcile(context.Background(), "project-1", "session-1"); err != nil || storeFake.session.Status != "FAILED" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})

	t.Run("missing runner session fails durable session", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"}
		service := reconcileBranchService(t, storeFake, &reconcileExecutionManager{transport: nil, active: false})
		if _, err := service.Reconcile(context.Background(), "project-1", "session-1"); err != nil || storeFake.session.Status != "FAILED" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})
}

func TestReconcileRetainedTerminalFailureBranches(t *testing.T) {
	tests := []struct {
		name       string
		waitErr    error
		wantStatus string
		wantErr    bool
	}{
		{name: "grace expired", waitErr: context.DeadlineExceeded, wantStatus: "FAILED"},
		{name: "runner disconnected", waitErr: runner.ErrDisconnected, wantStatus: "RUNNING", wantErr: true},
		{name: "generic retained error", waitErr: errors.New("retained delivery failed"), wantStatus: "FAILED", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeFake := newBranchExecutionStore()
			storeFake.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"}
			transport := newFakeExecutionTransport("session-1")
			transport.waitErr = tt.waitErr
			close(transport.resultCh)
			service := reconcileBranchService(t, storeFake, &reconcileExecutionManager{transport: transport, active: false})
			_, err := service.Reconcile(context.Background(), "project-1", "session-1")
			if (err != nil) != tt.wantErr || storeFake.session.Status != tt.wantStatus {
				t.Fatalf("session=%+v err=%v", storeFake.session, err)
			}
		})
	}
}

func TestReconcileActiveRunnerStatusFailure(t *testing.T) {
	storeFake := newBranchExecutionStore()
	storeFake.updateRunnerErr = errors.New("runner status failed")
	storeFake.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"}
	transport := newFakeExecutionTransport("session-1")
	service := reconcileBranchService(t, storeFake, &reconcileExecutionManager{transport: transport, active: true})
	if _, err := service.Reconcile(context.Background(), "project-1", "session-1"); err == nil {
		t.Fatal("expected runner status persistence error")
	}
	transport.result = runner.Result{ExitCode: 0}
	close(transport.resultCh)
}
