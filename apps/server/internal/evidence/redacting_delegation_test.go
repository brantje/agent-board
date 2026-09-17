package evidence

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationCapabilityStore struct {
	store.ControlPlaneStore

	requestCommand store.RequestDelegationCommand
	getProjectID   string
	getRunID       string
	listProjectID  string
	listParentRun  string
}

func (s *delegationCapabilityStore) RequestDelegation(_ context.Context, command store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	s.requestCommand = command
	return store.RequestDelegationResult{Delegation: store.Delegation{ID: "delegation", DelegatedRunID: "child-run"}}, nil
}

func (s *delegationCapabilityStore) GetDelegationByRun(_ context.Context, projectID, runID string) (store.Delegation, error) {
	s.getProjectID = projectID
	s.getRunID = runID
	return store.Delegation{ID: "delegation", DelegatedRunID: runID}, nil
}

func (s *delegationCapabilityStore) ListDelegationsByParentRun(_ context.Context, projectID, parentRunID string) ([]store.Delegation, error) {
	s.listProjectID = projectID
	s.listParentRun = parentRunID
	return []store.Delegation{{ID: "delegation", ParentRunID: parentRunID}}, nil
}

func TestRedactingStorePreservesDelegationCapability(t *testing.T) {
	base := &delegationCapabilityStore{}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())
	ctx := t.Context()

	command := store.RequestDelegationCommand{
		ProjectID:     "project",
		ParentRunID:   "parent-run",
		TargetAgentID: "target-agent",
		Task:          "inspect scheduler ownership",
		RequestKey:    "request-key",
	}
	created, err := wrapped.RequestDelegation(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if created.Delegation.ID != "delegation" || created.Delegation.DelegatedRunID != "child-run" {
		t.Fatalf("created delegation=%+v", created.Delegation)
	}
	if base.requestCommand != command {
		t.Fatalf("forwarded command=%+v want=%+v", base.requestCommand, command)
	}

	got, err := wrapped.GetDelegationByRun(ctx, "project", "child-run")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "delegation" || base.getProjectID != "project" || base.getRunID != "child-run" {
		t.Fatalf("get delegation=%+v project=%q run=%q", got, base.getProjectID, base.getRunID)
	}

	listed, err := wrapped.ListDelegationsByParentRun(ctx, "project", "parent-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != "delegation" || base.listProjectID != "project" || base.listParentRun != "parent-run" {
		t.Fatalf("list delegations=%+v project=%q parentRun=%q", listed, base.listProjectID, base.listParentRun)
	}
}

func TestRedactingStoreReportsMissingDelegationCapability(t *testing.T) {
	wrapped := NewRedactingStore(&captureStore{}, redaction.NewRegistry())
	ctx := t.Context()

	if _, err := wrapped.RequestDelegation(ctx, store.RequestDelegationCommand{}); err == nil {
		t.Fatal("expected missing delegation request capability error")
	}
	if _, err := wrapped.GetDelegationByRun(ctx, "project", "run"); err == nil {
		t.Fatal("expected missing delegation lookup capability error")
	}
	if _, err := wrapped.ListDelegationsByParentRun(ctx, "project", "run"); err == nil {
		t.Fatal("expected missing delegation list capability error")
	}
}
