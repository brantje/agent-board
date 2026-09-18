package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationServiceStore struct {
	store.ControlPlaneStore

	requestCommand store.RequestDelegationCommand
	requestResult  store.RequestDelegationResult
	requestErr     error

	getProjectID string
	getRunID     string
	getResult    store.Delegation
	getErr       error

	listProjectID   string
	listParentRunID string
	listResult      []store.Delegation
	listErr         error
}

func (s *delegationServiceStore) RequestDelegation(_ context.Context, command store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	s.requestCommand = command
	return s.requestResult, s.requestErr
}

func (s *delegationServiceStore) GetDelegationByRun(_ context.Context, projectID, runID string) (store.Delegation, error) {
	s.getProjectID = projectID
	s.getRunID = runID
	return s.getResult, s.getErr
}

func (s *delegationServiceStore) ListDelegationsByParentRun(_ context.Context, projectID, parentRunID string) ([]store.Delegation, error) {
	s.listProjectID = projectID
	s.listParentRunID = parentRunID
	return s.listResult, s.listErr
}

func (s *delegationServiceStore) GetRun(_ context.Context, projectID, runID string) (store.Run, error) {
	for _, delegation := range append([]store.Delegation{s.getResult}, s.listResult...) {
		if delegation.ProjectID != projectID {
			continue
		}
		if runID == delegation.ParentRunID {
			return store.Run{ID: runID, ProjectID: projectID, IssueID: delegation.IssueID, WorkspaceID: "workspace-1", Status: "PAUSED"}, nil
		}
		if runID == delegation.DelegatedRunID {
			return store.Run{ID: runID, ProjectID: projectID, IssueID: delegation.IssueID, WorkspaceID: "workspace-1", Status: "COMPLETED"}, nil
		}
	}
	return store.Run{}, store.ErrNotFound
}

func (s *delegationServiceStore) GetWorkspaceCurrentRevision(context.Context, string, string) (string, error) {
	return "revision-1", nil
}

type unsupportedDelegationStore struct {
	store.ControlPlaneStore
}

type delegationEventRecorder struct {
	published []store.Event
}

func (*delegationEventRecorder) Record(_ context.Context, event store.Event) (store.Event, error) {
	return event, nil
}

func (r *delegationEventRecorder) PublishPersisted(_ context.Context, event store.Event) {
	r.published = append(r.published, event)
}

func TestRequestDelegationMapsCommandAndPublishesPersistedEvents(t *testing.T) {
	backend := &delegationServiceStore{
		requestResult: store.RequestDelegationResult{
			Delegation: store.Delegation{ID: "delegation"},
			Events:     []store.Event{{ID: "event"}},
		},
	}
	recorder := &delegationEventRecorder{}
	service := New(backend)
	service.SetEventRecorder(recorder)

	input := DelegationRequest{TargetAgentID: "target", Task: "inspect scheduler ownership", RequestKey: "request-key"}
	result, err := service.RequestDelegation(context.Background(), "project", "parent-run", input)
	if err != nil {
		t.Fatal(err)
	}
	wantCommand := store.RequestDelegationCommand{
		ProjectID:     "project",
		ParentRunID:   "parent-run",
		TargetAgentID: input.TargetAgentID,
		Task:          input.Task,
		RequestKey:    input.RequestKey,
	}
	if backend.requestCommand != wantCommand {
		t.Fatalf("command = %+v, want %+v", backend.requestCommand, wantCommand)
	}
	if result.Delegation.ID != "delegation" {
		t.Fatalf("delegation id = %q", result.Delegation.ID)
	}
	if len(recorder.published) != 1 || recorder.published[0].ID != "event" {
		t.Fatalf("published events = %+v", recorder.published)
	}
}

func TestRequestDelegationRejectsUnavailableStoresAndTranslatesErrors(t *testing.T) {
	var nilService *Service
	if _, err := nilService.RequestDelegation(context.Background(), "project", "run", DelegationRequest{}); err == nil {
		t.Fatal("nil service accepted delegation")
	}
	if _, err := New(&unsupportedDelegationStore{}).RequestDelegation(context.Background(), "project", "run", DelegationRequest{}); err == nil {
		t.Fatal("store without delegation support accepted delegation")
	}

	backend := &delegationServiceStore{requestErr: store.ErrNotFound}
	_, err := New(backend).RequestDelegation(context.Background(), "project", "run", DelegationRequest{TargetAgentID: "target", Task: "task"})
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %#v, want translated not-found error", err)
	}
}

func TestDelegationQueriesUseDelegationStoreAndTranslateErrors(t *testing.T) {
	backend := &delegationServiceStore{
		getResult: store.Delegation{ID: "child-lineage", ProjectID: "project", IssueID: "issue", ParentRunID: "parent-run", DelegatedRunID: "child-run"},
		listResult: []store.Delegation{
			{ID: "first", ProjectID: "project", IssueID: "issue", ParentRunID: "parent-run", DelegatedRunID: "child-1"},
			{ID: "second", ProjectID: "project", IssueID: "issue", ParentRunID: "parent-run", DelegatedRunID: "child-2"},
		},
	}
	service := New(backend)

	got, err := service.GetDelegationByRun(context.Background(), "project", "child-run")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "child-lineage" || got.ParentRunStatus != "PAUSED" || got.DelegatedRunStatus != "COMPLETED" || got.WorkspaceRevision != "revision-1" || backend.getProjectID != "project" || backend.getRunID != "child-run" {
		t.Fatalf("get delegation = %+v, project=%q run=%q", got, backend.getProjectID, backend.getRunID)
	}

	listed, err := service.ListDelegationsByParentRun(context.Background(), "project", "parent-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != "first" || backend.listProjectID != "project" || backend.listParentRunID != "parent-run" {
		t.Fatalf("list delegations = %+v, project=%q parent=%q", listed, backend.listProjectID, backend.listParentRunID)
	}

	backend.getErr = store.ErrNotFound
	if _, err := service.GetDelegationByRun(context.Background(), "project", "missing"); err == nil || !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get error = %#v, want translated not-found error", err)
	}
	backend.listErr = store.ErrConflict
	if _, err := service.ListDelegationsByParentRun(context.Background(), "project", "parent-run"); err == nil || !errors.Is(err, store.ErrConflict) {
		t.Fatalf("list error = %#v, want translated conflict error", err)
	}

	unsupported := New(&unsupportedDelegationStore{})
	if _, err := unsupported.GetDelegationByRun(context.Background(), "project", "run"); err == nil {
		t.Fatal("get accepted store without delegation support")
	}
	if _, err := unsupported.ListDelegationsByParentRun(context.Background(), "project", "run"); err == nil {
		t.Fatal("list accepted store without delegation support")
	}
}
