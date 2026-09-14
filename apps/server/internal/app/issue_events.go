package app

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func issueCreatorActor(issue store.Issue) (json.RawMessage, error) {
	if issue.CreatedByType == nil || issue.CreatedByID == nil {
		return append(json.RawMessage(nil), store.EmptyObject...), nil
	}
	return evidence.EncodePayload(map[string]string{"type": *issue.CreatedByType, "id": *issue.CreatedByID})
}

func (s *Service) recordIssueEvent(ctx context.Context, eventType string, issue store.Issue, actor json.RawMessage, payload any) (store.Event, error) {
	if s == nil || s.events == nil {
		return store.Event{}, nil
	}
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return store.Event{}, err
	}
	if issue.LastEvent != nil && issue.LastEvent.Type == "run.created" {
		publisher, _ := s.events.(persistedEventPublisher)
		publishPersistedEvents(ctx, publisher, []store.Event{*issue.LastEvent})
	}
	if len(actor) == 0 {
		actor = store.EmptyObject
	}
	issueID := issue.ID
	return s.events.Record(ctx, store.Event{
		Type:      eventType,
		ProjectID: issue.ProjectID,
		IssueID:   &issueID,
		Actor:     actor,
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
