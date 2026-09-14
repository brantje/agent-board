package store

import "encoding/json"

func NewIssueCreatedEvent(issue Issue) (Event, error) {
	actor := append(json.RawMessage(nil), EmptyObject...)
	if issue.CreatedByType != nil && issue.CreatedByID != nil {
		encoded, err := json.Marshal(map[string]string{"type": *issue.CreatedByType, "id": *issue.CreatedByID})
		if err != nil {
			return Event{}, err
		}
		actor = encoded
	}
	return newIssueMutationEvent("issue.created", issue, actor, "")
}

func NewIssueUpdatedEvent(issue Issue, previousStatus string) (Event, error) {
	eventType := "issue.updated"
	if previousStatus != issue.Status {
		eventType = "issue.status_changed"
	}
	return newIssueMutationEvent(eventType, issue, EmptyObject, previousStatus)
}

func newIssueMutationEvent(eventType string, issue Issue, actor json.RawMessage, previousStatus string) (Event, error) {
	payload := map[string]any{
		"title":    issue.Title,
		"status":   issue.Status,
		"priority": issue.Priority,
	}
	if eventType == "issue.status_changed" {
		payload["previousStatus"] = previousStatus
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	issueID := issue.ID
	return Event{
		Type:      eventType,
		ProjectID: issue.ProjectID,
		IssueID:   &issueID,
		Actor:     append(json.RawMessage(nil), actor...),
		Payload:   encoded,
	}, nil
}
