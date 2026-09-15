package evidence

import (
	"context"
	"reflect"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueExecutionCapabilityStore struct {
	store.ControlPlaneStore
	startProjectID  string
	startIssueID    string
	run             store.Run
	event           store.Event
	reconcileFilter store.IssueExecutionFilter
	reconcileEvents []store.Event
	readinessFilter store.IssueExecutionFilter
	scopes          []store.IssueExecutionScope
	stateProjectID  string
	stateIssueID    string
	state           store.IssueExecutionState
}

func (s *issueExecutionCapabilityStore) StartIssueRun(_ context.Context, projectID, issueID string) (store.Run, store.Event, error) {
	s.startProjectID = projectID
	s.startIssueID = issueID
	return s.run, s.event, nil
}

func (s *issueExecutionCapabilityStore) ReconcileIssueExecution(_ context.Context, filter store.IssueExecutionFilter) ([]store.Event, error) {
	s.reconcileFilter = filter
	return s.reconcileEvents, nil
}

func (s *issueExecutionCapabilityStore) RunnableIssueExecutionScopes(_ context.Context, filter store.IssueExecutionFilter) ([]store.IssueExecutionScope, error) {
	s.readinessFilter = filter
	return s.scopes, nil
}

func (s *issueExecutionCapabilityStore) GetIssueExecutionState(_ context.Context, projectID, issueID string) (store.IssueExecutionState, error) {
	s.stateProjectID = projectID
	s.stateIssueID = issueID
	return s.state, nil
}

func TestRedactingStorePreservesIssueExecutionCapabilities(t *testing.T) {
	activeRun := store.Run{ID: "run-active", ProjectID: "project-1", IssueID: "issue-1", Status: "RUNNING"}
	base := &issueExecutionCapabilityStore{
		run:             store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1", Status: "QUEUED"},
		event:           store.Event{ID: "event-1", ProjectID: "project-1"},
		reconcileEvents: []store.Event{{ID: "event-2", ProjectID: "project-1"}},
		scopes:          []store.IssueExecutionScope{{ProjectID: "project-1", AgentID: "agent-1"}},
		state:           store.IssueExecutionState{State: store.IssueExecutionActive, ActiveRun: &activeRun},
	}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())
	ctx := t.Context()

	run, event, err := wrapped.StartIssueRun(ctx, "project-1", "issue-1")
	if err != nil {
		t.Fatalf("StartIssueRun() error=%v", err)
	}
	if run.ID != base.run.ID || event.ID != base.event.ID || base.startProjectID != "project-1" || base.startIssueID != "issue-1" {
		t.Fatalf("run=%+v event=%+v project=%q issue=%q", run, event, base.startProjectID, base.startIssueID)
	}

	filter := store.IssueExecutionFilter{ProjectID: "project-1", AgentID: "agent-1", ModelProfileID: "model-1", ProviderID: "provider-1"}
	events, err := wrapped.ReconcileIssueExecution(ctx, filter)
	if err != nil {
		t.Fatalf("ReconcileIssueExecution() error=%v", err)
	}
	if !reflect.DeepEqual(events, base.reconcileEvents) || base.reconcileFilter != filter {
		t.Fatalf("events=%+v filter=%+v", events, base.reconcileFilter)
	}

	scopes, err := wrapped.RunnableIssueExecutionScopes(ctx, filter)
	if err != nil {
		t.Fatalf("RunnableIssueExecutionScopes() error=%v", err)
	}
	if !reflect.DeepEqual(scopes, base.scopes) || base.readinessFilter != filter {
		t.Fatalf("scopes=%+v filter=%+v", scopes, base.readinessFilter)
	}

	state, err := wrapped.GetIssueExecutionState(ctx, "project-1", "issue-1")
	if err != nil {
		t.Fatalf("GetIssueExecutionState() error=%v", err)
	}
	if state.State != store.IssueExecutionActive || state.ActiveRun == nil || state.ActiveRun.ID != activeRun.ID || base.stateProjectID != "project-1" || base.stateIssueID != "issue-1" {
		t.Fatalf("state=%+v project=%q issue=%q", state, base.stateProjectID, base.stateIssueID)
	}
}

func TestRedactingStoreReportsMissingIssueExecutionCapabilities(t *testing.T) {
	wrapped := NewRedactingStore(&captureStore{}, redaction.NewRegistry())
	ctx := t.Context()

	if _, _, err := wrapped.StartIssueRun(ctx, "project-1", "issue-1"); err == nil {
		t.Fatal("expected missing Issue execution capability error")
	}
	if _, err := wrapped.ReconcileIssueExecution(ctx, store.IssueExecutionFilter{}); err == nil {
		t.Fatal("expected missing Issue execution capability error")
	}
	if _, err := wrapped.RunnableIssueExecutionScopes(ctx, store.IssueExecutionFilter{}); err == nil {
		t.Fatal("expected missing Issue execution readiness capability error")
	}
	if _, err := wrapped.GetIssueExecutionState(ctx, "project-1", "issue-1"); err == nil {
		t.Fatal("expected missing Issue execution state capability error")
	}
}
