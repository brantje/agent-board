package runexec

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationCapabilityStore struct {
	store.ControlPlaneStore
	got    store.RequestDelegationCommand
	result store.RequestDelegationResult
}

func (s *delegationCapabilityStore) RequestDelegation(_ context.Context, input store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	s.got = input
	return s.result, nil
}

func (s *delegationCapabilityStore) GetDelegationByRun(context.Context, string, string) (store.Delegation, error) {
	return store.Delegation{}, store.ErrNotFound
}

func (s *delegationCapabilityStore) ListDelegationsByParentRun(context.Context, string, string) ([]store.Delegation, error) {
	return nil, nil
}

func (s *delegationCapabilityStore) SetIssueStatus(context.Context, store.IssueStatusMutation) (store.IssueMutationResult, error) {
	return store.IssueMutationResult{}, nil
}

func TestEngineRequestDelegationCapabilityDerivesParentIdentity(t *testing.T) {
	storage := &delegationCapabilityStore{result: store.RequestDelegationResult{
		Delegation:   store.Delegation{ID: "delegation-1"},
		DelegatedRun: store.Run{ID: "delegated-run-1"},
	}}
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run:     executioncontext.RunContext{ID: "parent-run-1"},
		Agent:   executioncontext.AgentContext{ID: "parent-agent-1", AllowDelegation: true},
	}
	processor := &Processor{store: storage}
	request, err := processor.engineRequest(t.Context(), safe, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Delegation == nil {
		t.Fatal("allowed authoritative Run did not receive delegation capability")
	}
	if request.IssueStatus == nil {
		t.Fatal("authoritative Run unexpectedly lost Issue status capability")
	}
	result, err := request.Delegation.Delegate(t.Context(), engine.DelegationRequest{
		TargetAgentID: "target-agent-1", Task: "bounded task", RequestKey: "tool-part-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "delegation-1" || result.RunID != "delegated-run-1" {
		t.Fatalf("delegation result=%+v", result)
	}
	if storage.got.ProjectID != safe.Project.ID || storage.got.ParentRunID != safe.Run.ID || storage.got.TargetAgentID != "target-agent-1" || storage.got.Task != "bounded task" || storage.got.RequestKey != "tool-part-1" {
		t.Fatalf("canonical command=%+v", storage.got)
	}
}

func TestEngineRequestDelegationCapabilityIsPolicyAndAuthorityGated(t *testing.T) {
	storage := &delegationCapabilityStore{}
	processor := &Processor{store: storage}
	base := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run:     executioncontext.RunContext{ID: "run-1"},
		Agent:   executioncontext.AgentContext{ID: "agent-1"},
	}

	request, err := processor.engineRequest(t.Context(), base, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Delegation != nil {
		t.Fatal("allowDelegation=false exposed delegation capability")
	}
	if request.IssueStatus == nil {
		t.Fatal("normal Run should retain Issue status capability")
	}

	delegated := base
	delegated.Agent.AllowDelegation = true
	delegated.Delegation = &executioncontext.DelegationContext{
		ID: "d1", ParentRunID: "parent", ParentAgentID: "parent-agent", TargetAgentID: delegated.Agent.ID,
		Task: "bounded task", RequestKey: "part-1",
	}
	request, err = processor.engineRequest(t.Context(), delegated, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Delegation != nil {
		t.Fatal("delegated Run exposed nested delegation capability")
	}
	if request.IssueStatus != nil {
		t.Fatal("delegated Run exposed authoritative Issue status capability")
	}
}
