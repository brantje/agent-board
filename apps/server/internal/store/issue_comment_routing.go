package store

import "strings"

const (
	IssueCommentImplicitRoutingReasonDirectAgentReply = "DIRECT_AGENT_REPLY"
	IssueCommentImplicitRoutingReasonUniqueThreadAgent = "UNIQUE_THREAD_AGENT"
	IssueCommentImplicitRoutingReasonIssueAssignee      = "ISSUE_ASSIGNEE"

	IssueCommentImplicitOutcomeQueued     = "QUEUED"
	IssueCommentImplicitOutcomeCoalesced  = "COALESCED"
	IssueCommentImplicitOutcomeDeferred   = "DEFERRED"
	IssueCommentImplicitOutcomeBlocked    = "BLOCKED"
	IssueCommentImplicitOutcomeSuppressed = "SUPPRESSED"

	IssueCommentImplicitReasonWorkflowBlocked = "WORKFLOW_BLOCKED"
)

type IssueCommentImplicitRoutingFacts struct {
	HasExplicitMentions bool
	IsReply             bool
	ParentAuthorType    string
	ParentAuthorID      string
	ThreadAgentIDs      []string
	AssigneeType        *string
	AssigneeID          *string
}

type IssueCommentImplicitRoute struct {
	TargetAgentID string
	RoutingReason string
}

func ResolveIssueCommentImplicitRoute(facts IssueCommentImplicitRoutingFacts) (IssueCommentImplicitRoute, bool) {
	if facts.HasExplicitMentions {
		return IssueCommentImplicitRoute{}, false
	}
	if facts.IsReply && facts.ParentAuthorType == ActorTypeAgent {
		if target := strings.TrimSpace(facts.ParentAuthorID); target != "" {
			return IssueCommentImplicitRoute{
				TargetAgentID: target,
				RoutingReason: IssueCommentImplicitRoutingReasonDirectAgentReply,
			}, true
		}
	}

	if facts.IsReply {
		if target, ok := uniqueNonEmptyString(facts.ThreadAgentIDs); ok {
			return IssueCommentImplicitRoute{
				TargetAgentID: target,
				RoutingReason: IssueCommentImplicitRoutingReasonUniqueThreadAgent,
			}, true
		}
		return IssueCommentImplicitRoute{}, false
	}

	if facts.AssigneeType != nil && facts.AssigneeID != nil && *facts.AssigneeType == "AGENT" {
		if target := strings.TrimSpace(*facts.AssigneeID); target != "" {
			return IssueCommentImplicitRoute{
				TargetAgentID: target,
				RoutingReason: IssueCommentImplicitRoutingReasonIssueAssignee,
			}, true
		}
	}
	return IssueCommentImplicitRoute{}, false
}

func IssueCommentImplicitWakeAllowed(status string) bool {
	switch strings.TrimSpace(status) {
	case "BACKLOG", "DONE":
		return false
	default:
		return true
	}
}

func uniqueNonEmptyString(values []string) (string, bool) {
	var selected string
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if selected == "" {
			selected = value
			continue
		}
		if value != selected {
			return "", false
		}
	}
	return selected, selected != ""
}
