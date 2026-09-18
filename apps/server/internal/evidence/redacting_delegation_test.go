package evidence

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationCapabilityStore struct {
	store.ControlPlaneStore

	requestCommand      store.RequestDelegationCommand
	getProjectID        string
	getRunID            string
	listProjectID       string
	listParentRun       string
	continuationProject string
	continuationParent  string
	continuationJob     string
	targetProject       string
	targetParentAgent   string
}

func (s *delegationCapabilityStore) ListDelegationTargets(_ context.Context, projectID, parentAgentID string) ([]store.DelegationTarget, error) {
	s.targetProject = projectID
	s.targetParentAgent = parentAgentID
	return []store.DelegationTarget{{ID: "target-agent", Name: "Target Agent"}}, nil
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

func (s *delegationCapabilityStore) GetDelegationByContinuationJob(_ context.Context, projectID, parentRunID, jobID string) (store.Delegation, error) {
	s.continuationProject = projectID
	s.continuationParent = parentRunID
	s.continuationJob = jobID
	return store.Delegation{ID: "delegation", ParentRunID: parentRunID, ContinuationJobID: &jobID}, nil
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

	targets, err := wrapped.ListDelegationTargets(ctx, "project", "parent-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "target-agent" || base.targetProject != "project" || base.targetParentAgent != "parent-agent" {
		t.Fatalf("delegation targets=%+v project=%q parentAgent=%q", targets, base.targetProject, base.targetParentAgent)
	}

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

	continuation, err := wrapped.GetDelegationByContinuationJob(ctx, "project", "parent-run", "resume-job")
	if err != nil {
		t.Fatal(err)
	}
	if continuation.ID != "delegation" || continuation.ContinuationJobID == nil || *continuation.ContinuationJobID != "resume-job" || base.continuationProject != "project" || base.continuationParent != "parent-run" || base.continuationJob != "resume-job" {
		t.Fatalf("continuation delegation=%+v project=%q parentRun=%q job=%q", continuation, base.continuationProject, base.continuationParent, base.continuationJob)
	}

	listed, err := wrapped.ListDelegationsByParentRun(ctx, "project", "parent-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != "delegation" || base.listProjectID != "project" || base.listParentRun != "parent-run" {
		t.Fatalf("list delegations=%+v project=%q parentRun=%q", listed, base.listProjectID, base.listParentRun)
	}
}


func TestRedactingStoreRedactsDelegationTaskBeforePersistenceBoundary(t *testing.T) {
	base := &delegationCapabilityStore{}
	registry := redaction.NewRegistry()
	registry.Register("parent-run", []string{"super-secret-token"})
	wrapped := NewRedactingStore(base, registry)

	command := store.RequestDelegationCommand{
		ProjectID: "project", ParentRunID: "parent-run", TargetAgentID: "target-agent",
		Task: "inspect value super-secret-token without changing behavior", RequestKey: "request-key",
	}
	if _, err := wrapped.RequestDelegation(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if base.requestCommand.ProjectID != command.ProjectID ||
		base.requestCommand.ParentRunID != command.ParentRunID ||
		base.requestCommand.TargetAgentID != command.TargetAgentID ||
		base.requestCommand.RequestKey != command.RequestKey {
		t.Fatalf("delegation identity changed during redaction: %+v", base.requestCommand)
	}
	if base.requestCommand.Task == command.Task || base.requestCommand.Task == "" {
		t.Fatalf("delegation task was not redacted: %q", base.requestCommand.Task)
	}
	if strings.Contains(base.requestCommand.Task, "super-secret-token") {
		t.Fatalf("delegation task leaked secret: %q", base.requestCommand.Task)
	}

	plain := command
	plain.RequestKey = "plain-request"
	plain.Task = "inspect scheduler ownership"
	if _, err := wrapped.RequestDelegation(t.Context(), plain); err != nil {
		t.Fatal(err)
	}
	if base.requestCommand.Task != plain.Task {
		t.Fatalf("ordinary delegation task changed: got %q want %q", base.requestCommand.Task, plain.Task)
	}
}

func TestRedactingStorePreservesDelegationTaskLengthValidationBeforeRedaction(t *testing.T) {
	base := &delegationCapabilityStore{}
	registry := redaction.NewRegistry()
	secret := strings.Repeat("s", store.MaxDelegationTaskCharacters+1)
	registry.Register("parent-run", []string{secret})
	wrapped := NewRedactingStore(base, registry)

	_, err := wrapped.RequestDelegation(t.Context(), store.RequestDelegationCommand{
		ProjectID: "project", ParentRunID: "parent-run", TargetAgentID: "target-agent",
		Task: secret, RequestKey: "oversized-secret",
	})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("oversized delegation task error=%v want ErrInvalidArgument", err)
	}
	if base.requestCommand.RequestKey != "" {
		t.Fatalf("oversized task reached durable base store: %+v", base.requestCommand)
	}
}

func TestRedactingStoreReportsMissingDelegationCapability(t *testing.T) {
	wrapped := NewRedactingStore(&captureStore{}, redaction.NewRegistry())
	ctx := t.Context()

	if _, err := wrapped.ListDelegationTargets(ctx, "project", "parent-agent"); err == nil {
		t.Fatal("expected missing delegation target capability error")
	}
	if _, err := wrapped.RequestDelegation(ctx, store.RequestDelegationCommand{}); err == nil {
		t.Fatal("expected missing delegation request capability error")
	}
	if _, err := wrapped.GetDelegationByRun(ctx, "project", "run"); err == nil {
		t.Fatal("expected missing delegation lookup capability error")
	}
	if _, err := wrapped.GetDelegationByContinuationJob(ctx, "project", "parent-run", "resume-job"); err == nil {
		t.Fatal("expected missing delegation continuation capability error")
	}
	if _, err := wrapped.ListDelegationsByParentRun(ctx, "project", "run"); err == nil {
		t.Fatal("expected missing delegation list capability error")
	}
}
