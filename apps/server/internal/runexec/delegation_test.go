package runexec

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationCapabilityStore struct {
	store.ControlPlaneStore
	got      store.RequestDelegationCommand
	result   store.RequestDelegationResult
	appended []store.Event
	targets  []store.DelegationTarget
	issue    *store.Issue
	squad    *store.Squad
	agents   map[string]store.Agent
}

func (s *delegationCapabilityStore) ListDelegationTargets(context.Context, string, string) ([]store.DelegationTarget, error) {
	if s.targets == nil {
		return []store.DelegationTarget{{ID: "target-agent-1", Name: "Target agent"}}, nil
	}
	return append([]store.DelegationTarget(nil), s.targets...), nil
}

func (s *delegationCapabilityStore) GetIssue(_ context.Context, _, _ string) (store.Issue, error) {
	if s.issue == nil {
		return store.Issue{}, store.ErrNotFound
	}
	return *s.issue, nil
}

func (s *delegationCapabilityStore) GetSquad(_ context.Context, _, _ string) (store.Squad, error) {
	if s.squad == nil {
		return store.Squad{}, store.ErrNotFound
	}
	return *s.squad, nil
}

func (s *delegationCapabilityStore) GetAgentInScope(_ context.Context, _ *string, id string) (store.Agent, error) {
	if agent, ok := s.agents[id]; ok {
		return agent, nil
	}
	return store.Agent{}, store.ErrNotFound
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

func (s *delegationCapabilityStore) AppendEvent(_ context.Context, event store.Event) (store.Event, error) {
	s.appended = append(s.appended, event)
	return event, nil
}

type delegationEventPublisher struct {
	events []store.Event
}

func (p *delegationEventPublisher) Publish(_ context.Context, event store.Event) error {
	p.events = append(p.events, event)
	return nil
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

func TestEngineRequestDelegationContextIncludesTargetsAndSquadMembers(t *testing.T) {
	role := "implementation"
	issueType, squadID := "SQUAD", "squad-1"
	storage := &delegationCapabilityStore{
		targets: []store.DelegationTarget{
			{ID: "other-agent", Name: "Other agent"},
		},
		issue: &store.Issue{ID: "issue-1", ProjectID: "project-1", AssigneeType: &issueType, AssigneeID: &squadID},
		squad: &store.Squad{
			ID: "squad-1", ProjectID: "project-1", Name: "Backend", LeaderAgentID: "parent-agent-1",
			Members: []store.SquadMember{
				{Type: store.SquadMemberTypeAgent, ID: "member-agent", Role: &role},
				{Type: store.SquadMemberTypeUser, ID: "user-1"},
			},
		},
		agents: map[string]store.Agent{
			"parent-agent-1": {ID: "parent-agent-1", Name: "Leader"},
			"member-agent":   {ID: "member-agent", Name: "Member agent"},
		},
	}
	processor := &Processor{store: storage}
	request, err := processor.engineRequest(t.Context(), executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue:   executioncontext.IssueContext{ID: "issue-1"},
		Run:     executioncontext.RunContext{ID: "parent-run-1"},
		Agent:   executioncontext.AgentContext{ID: "parent-agent-1", AllowDelegation: true},
	}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Delegation == nil || request.DelegationContext == nil {
		t.Fatal("authoritative delegating Run did not receive delegation tool context")
	}
	if len(request.DelegationContext.Targets) != 1 || request.DelegationContext.Targets[0].ID != "other-agent" {
		t.Fatalf("targets=%+v", request.DelegationContext.Targets)
	}
	squad := request.DelegationContext.Squad
	if squad == nil || squad.ID != "squad-1" || squad.LeaderAgentID != "parent-agent-1" || squad.LeaderAgentName != "Leader" {
		t.Fatalf("squad context=%+v", squad)
	}
	if len(squad.Members) != 1 || squad.Members[0].ID != "member-agent" || squad.Members[0].Role == nil || *squad.Members[0].Role != role {
		t.Fatalf("squad members=%+v", squad.Members)
	}
	if squad.Members[0].ID == request.DelegationContext.Targets[0].ID {
		t.Fatal("Squad membership was incorrectly treated as delegation target authorization")
	}
}

func TestEngineRequestDelegationContextKeepsDirectAgentOwnershipOutOfSquadContext(t *testing.T) {
	issueType, agentID := "AGENT", "parent-agent-1"
	storage := &delegationCapabilityStore{
		targets: []store.DelegationTarget{{ID: "target-agent-1", Name: "Target agent"}},
		issue:   &store.Issue{ID: "issue-1", ProjectID: "project-1", AssigneeType: &issueType, AssigneeID: &agentID},
	}
	request, err := (&Processor{store: storage}).engineRequest(t.Context(), executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue:   executioncontext.IssueContext{ID: "issue-1"},
		Run:     executioncontext.RunContext{ID: "parent-run-1"},
		Agent:   executioncontext.AgentContext{ID: "parent-agent-1", AllowDelegation: true},
	}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if request.DelegationContext == nil || len(request.DelegationContext.Targets) != 1 {
		t.Fatalf("delegation context=%+v", request.DelegationContext)
	}
	if request.DelegationContext.Squad != nil {
		t.Fatalf("direct Agent-owned Issue exposed Squad context: %+v", request.DelegationContext.Squad)
	}
}

func TestEngineRequestDelegationPublishesPersistedEvent(t *testing.T) {
	persisted := store.Event{ID: "event-1", Type: "delegation.created", ProjectID: "project-1"}
	storage := &delegationCapabilityStore{result: store.RequestDelegationResult{
		Delegation:   store.Delegation{ID: "delegation-1"},
		DelegatedRun: store.Run{ID: "delegated-run-1"},
		Events:       []store.Event{persisted},
	}}
	publisher := &delegationEventPublisher{}
	recorder, err := evidence.NewRecorder(storage, publisher)
	if err != nil {
		t.Fatal(err)
	}
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run:     executioncontext.RunContext{ID: "parent-run-1"},
		Agent:   executioncontext.AgentContext{ID: "parent-agent-1", AllowDelegation: true},
	}
	processor := &Processor{store: storage, events: recorder}
	request, err := processor.engineRequest(t.Context(), safe, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Delegation == nil {
		t.Fatal("delegation capability is unavailable")
	}
	if _, err := request.Delegation.Delegate(t.Context(), engine.DelegationRequest{
		TargetAgentID: "target-agent-1", Task: "bounded task", RequestKey: "tool-call-1",
	}); err != nil {
		t.Fatal(err)
	}
	if len(storage.appended) != 0 {
		t.Fatalf("persisted delegation event was appended again: %+v", storage.appended)
	}
	if len(publisher.events) != 1 || publisher.events[0].ID != persisted.ID {
		t.Fatalf("published events=%+v", publisher.events)
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
	if request.Delegation != nil || request.DelegationContext != nil {
		t.Fatal("allowDelegation=false exposed delegation capability or context")
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
	if request.Delegation != nil || request.DelegationContext != nil {
		t.Fatal("delegated Run exposed nested delegation capability or context")
	}
	if request.IssueStatus != nil {
		t.Fatal("delegated Run exposed authoritative Issue status capability")
	}
}
