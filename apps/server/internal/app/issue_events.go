package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Service) recordIssueEvent(ctx context.Context, eventType string, issue store.Issue, payload any) (store.Event, error) {
	if s == nil || s.events == nil {
		return store.Event{}, nil
	}
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return store.Event{}, err
	}
	issueID := issue.ID
	return s.events.Record(ctx, store.Event{
		Type:      eventType,
		ProjectID: issue.ProjectID,
		IssueID:   &issueID,
		Actor:     store.EmptyObject,
		Payload:   encoded,
	})
}

func attachIssueEvent(issue store.Issue, event store.Event) store.Issue {
	if event.ID == "" {
		return issue
	}
	issue.LastEvent = &event
	return issue
}

func issueMutationPayload(issue store.Issue) map[string]any {
	return map[string]any{
		"title":    issue.Title,
		"status":   issue.Status,
		"priority": issue.Priority,
	}
}
