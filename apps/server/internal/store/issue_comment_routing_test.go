package store

import "testing"

func TestResolveIssueCommentImplicitRoute(t *testing.T) {
	agentA, agentB := "agent-a", "agent-b"
	agentType, userType := "AGENT", "USER"

	tests := []struct {
		name   string
		facts  IssueCommentImplicitRoutingFacts
		target string
		reason string
		ok     bool
	}{
		{
			name: "explicit mention disables implicit routing",
			facts: IssueCommentImplicitRoutingFacts{
				HasExplicitMentions: true,
				IsReply: true, ParentAuthorType: ActorTypeAgent, ParentAuthorID: agentA,
				ThreadAgentIDs: []string{agentA}, AssigneeType: &agentType, AssigneeID: &agentB,
			},
		},
		{
			name: "direct Agent reply wins over ambiguous thread",
			facts: IssueCommentImplicitRoutingFacts{
				IsReply: true, ParentAuthorType: ActorTypeAgent, ParentAuthorID: agentB,
				ThreadAgentIDs: []string{agentA, agentB}, AssigneeType: &agentType, AssigneeID: &agentA,
			},
			target: agentB, reason: IssueCommentImplicitRoutingReasonDirectAgentReply, ok: true,
		},
		{
			name: "unique thread Agent wins over assignee",
			facts: IssueCommentImplicitRoutingFacts{
				IsReply: true, ParentAuthorType: ActorTypeHuman, ParentAuthorID: "user",
				ThreadAgentIDs: []string{agentA, agentA}, AssigneeType: &agentType, AssigneeID: &agentB,
			},
			target: agentA, reason: IssueCommentImplicitRoutingReasonUniqueThreadAgent, ok: true,
		},
		{
			name: "ambiguous thread does not fall back to assignee",
			facts: IssueCommentImplicitRoutingFacts{
				IsReply: true, ParentAuthorType: ActorTypeHuman, ParentAuthorID: "user",
				ThreadAgentIDs: []string{agentA, agentB}, AssigneeType: &agentType, AssigneeID: &agentA,
			},
		},
		{
			name: "reply without Agent participant does not fall back to assignee",
			facts: IssueCommentImplicitRoutingFacts{
				IsReply: true, ParentAuthorType: ActorTypeHuman, ParentAuthorID: "user",
				AssigneeType: &agentType, AssigneeID: &agentA,
			},
		},
		{
			name: "top level falls back to Agent assignee",
			facts: IssueCommentImplicitRoutingFacts{
				AssigneeType: &agentType, AssigneeID: &agentA,
			},
			target: agentA, reason: IssueCommentImplicitRoutingReasonIssueAssignee, ok: true,
		},
		{
			name: "top level User assignee has no implicit target",
			facts: IssueCommentImplicitRoutingFacts{
				AssigneeType: &userType, AssigneeID: &agentA,
			},
		},
		{name: "unassigned top level has no implicit target"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ResolveIssueCommentImplicitRoute(test.facts)
			if ok != test.ok {
				t.Fatalf("ok=%v want %v route=%+v", ok, test.ok, got)
			}
			if got.TargetAgentID != test.target || got.RoutingReason != test.reason {
				t.Fatalf("route=%+v want target=%q reason=%q", got, test.target, test.reason)
			}
		})
	}
}

func TestIssueCommentImplicitWakeAllowed(t *testing.T) {
	for _, status := range []string{"BACKLOG", "DONE"} {
		if IssueCommentImplicitWakeAllowed(status) {
			t.Fatalf("%s must not implicitly wake Agents", status)
		}
	}
	for _, status := range []string{"TODO", "IN_PROGRESS", "BLOCKED", "REVIEW"} {
		if !IssueCommentImplicitWakeAllowed(status) {
			t.Fatalf("%s should permit implicit wake evaluation", status)
		}
	}
}
