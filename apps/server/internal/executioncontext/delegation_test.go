package executioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationAwareStore struct {
	fakeStore
	delegation store.Delegation
	err        error
}

func (s delegationAwareStore) GetDelegationByRun(_ context.Context, projectID, runID string) (store.Delegation, error) {
	if s.err != nil {
		return store.Delegation{}, s.err
	}
	if s.delegation.ProjectID != projectID || s.delegation.DelegatedRunID != runID {
		return store.Delegation{}, store.ErrNotFound
	}
	return s.delegation, nil
}

func TestResolveIncludesDelegationLineageAndAgentPolicy(t *testing.T) {
	values := validStore()
	values.agent.AllowDelegation = true
	values.run.Attempt = 3
	delegation := store.Delegation{
		ID: "d1", ProjectID: values.project.ID, IssueID: values.issue.ID,
		ParentRunID: "parent-run", ParentAgentID: "parent-agent", TargetAgentID: values.agent.ID,
		Task: "bounded delegated task", DelegatedRunID: values.run.ID, RequestKey: "part-1",
	}
	resolver, err := NewResolver(delegationAwareStore{fakeStore: values, delegation: delegation})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve(t.Context(), values.project.ID, values.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Safe.Agent.AllowDelegation {
		t.Fatal("Agent allowDelegation policy was not resolved")
	}
	if resolved.Safe.Delegation == nil {
		t.Fatal("delegated Run lost durable lineage")
	}
	got := resolved.Safe.Delegation
	if got.ID != delegation.ID || got.ParentRunID != delegation.ParentRunID || got.ParentAgentID != delegation.ParentAgentID || got.TargetAgentID != values.agent.ID || got.Task != delegation.Task || got.RequestKey != delegation.RequestKey {
		t.Fatalf("delegation context=%+v", got)
	}
}

func TestResolveRejectsInconsistentDelegationLineage(t *testing.T) {
	values := validStore()
	delegation := store.Delegation{
		ID: "d1", ProjectID: values.project.ID, IssueID: values.issue.ID,
		ParentRunID: "parent-run", ParentAgentID: "parent-agent", TargetAgentID: "different-agent",
		Task: "bounded task", DelegatedRunID: values.run.ID, RequestKey: "part-1",
	}
	resolver, err := NewResolver(delegationAwareStore{fakeStore: values, delegation: delegation})
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(t.Context(), values.project.ID, values.run.ID)
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_configuration_invalid" {
		t.Fatalf("err=%#v", err)
	}
}

func TestResolveTreatsMissingDelegationAsNormalRun(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(delegationAwareStore{fakeStore: values, err: store.ErrNotFound})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve(t.Context(), values.project.ID, values.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Safe.Delegation != nil {
		t.Fatalf("normal Run delegation=%+v", resolved.Safe.Delegation)
	}
}

func TestResolveFailsClosedWhenDelegationLineageCannotBeRead(t *testing.T) {
	values := validStore()
	lookupErr := errors.New("delegation storage unavailable")
	resolver, err := NewResolver(delegationAwareStore{fakeStore: values, err: lookupErr})
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(t.Context(), values.project.ID, values.run.ID)
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_delegation_unavailable" || !errors.Is(err, lookupErr) {
		t.Fatalf("err=%#v", err)
	}
}
