package runexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationTargetContextStore struct {
	targets []store.DelegationTarget
	err     error
}

func (s *delegationTargetContextStore) ListDelegationTargets(context.Context, string, string) ([]store.DelegationTarget, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]store.DelegationTarget(nil), s.targets...), nil
}

func TestResolveDelegationToolContextFailsClosedWithoutTargetCapability(t *testing.T) {
	_, err := resolveDelegationToolContext(t.Context(), struct{}{}, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Agent:   executioncontext.AgentContext{ID: "agent-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "delegation target context is unavailable") {
		t.Fatalf("error=%v", err)
	}
}

func TestResolveDelegationToolContextWrapsTargetDiscoveryFailure(t *testing.T) {
	_, err := resolveDelegationToolContext(t.Context(), &delegationTargetContextStore{err: store.ErrConflict}, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Agent:   executioncontext.AgentContext{ID: "agent-1"},
	})
	if !errors.Is(err, store.ErrConflict) || !strings.Contains(err.Error(), "resolve delegation targets") {
		t.Fatalf("error=%v", err)
	}
}

func TestResolveDelegationToolContextRequiresSquadLookupForIssueContext(t *testing.T) {
	_, err := resolveDelegationToolContext(t.Context(), &delegationTargetContextStore{}, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue:   executioncontext.IssueContext{ID: "issue-1"},
		Agent:   executioncontext.AgentContext{ID: "agent-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "delegation Squad context is unavailable") {
		t.Fatalf("error=%v", err)
	}
}

func TestResolveDelegationToolContextPreservesMissingSquadMemberRole(t *testing.T) {
	issueType, squadID := "SQUAD", "squad-1"
	storage := &delegationCapabilityStore{
		targets: []store.DelegationTarget{{ID: "member-agent", Name: "Member agent"}},
		issue:   &store.Issue{ID: "issue-1", ProjectID: "project-1", AssigneeType: &issueType, AssigneeID: &squadID},
		squad: &store.Squad{
			ID: "squad-1", ProjectID: "project-1", Name: "Backend", LeaderAgentID: "leader-agent",
			Members: []store.SquadMember{{Type: store.SquadMemberTypeAgent, ID: "member-agent"}},
		},
		agents: map[string]store.Agent{
			"leader-agent": {ID: "leader-agent", Name: "Leader"},
			"member-agent": {ID: "member-agent", Name: "Member agent"},
		},
	}
	context, err := resolveDelegationToolContext(t.Context(), storage, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue:   executioncontext.IssueContext{ID: "issue-1"},
		Agent:   executioncontext.AgentContext{ID: "leader-agent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if context.Squad == nil || len(context.Squad.Members) != 1 {
		t.Fatalf("context=%+v", context)
	}
	if context.Squad.Members[0].Role != nil {
		t.Fatalf("missing role became non-nil: %+v", context.Squad.Members[0])
	}
}
