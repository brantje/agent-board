package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// AssignIssue is the compatibility Agent assignment entry point. The store uses
// the same transactional enqueue policy as public ownership mutations.
func (s *Service) AssignIssue(ctx context.Context, projectID, issueID, agentID string) (store.Issue, store.Run, error) {
	issue, err := s.GetIssue(ctx, projectID, issueID)
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}
	agent, err := s.GetAgent(ctx, &projectID, agentID)
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}

	assigned, run, err := s.store.AssignIssue(ctx, projectID, issueID, agentID)
	if err != nil {
		return store.Issue{}, store.Run{}, translateStoreError(err, "issue")
	}
	if issue.AssigneeID != nil && *issue.AssigneeID == agentID {
		assigned.LastEvent = issue.LastEvent
		return assigned, run, nil
	}
	event, err := s.recordIssueEvent(ctx, "issue.assigned", assigned, store.EmptyObject, map[string]any{"assignedTo": &store.Assignee{Type: "AGENT", ID: agentID, Name: agent.Name}})
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}
	return attachIssueEvent(assigned, event), run, nil
}
