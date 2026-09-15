package mcpapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func projectDTO(value store.Project) ProjectDTO {
	return ProjectDTO{ID: value.ID, Name: value.Name, IssuePrefix: value.IssuePrefix, SourceType: value.SourceType, DefaultBranch: value.DefaultBranch, AllowInternalRunner: value.AllowInternalRunner, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func agentDTO(value store.Agent) AgentDTO {
	return AgentDTO{ID: value.ID, ProjectID: value.ProjectID, Name: value.Name, RoleInstructions: value.RoleInstructions, Engine: value.Engine, ModelProfileID: value.ModelProfileID, ConcurrencyLimit: value.ConcurrencyLimit, State: value.State, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func assigneeDTO(value store.Assignee) AssigneeDTO {
	return AssigneeDTO{Type: value.Type, ID: value.ID, Name: value.Name}
}

func issueDTO(value store.Issue) IssueDTO {
	out := IssueDTO{ID: value.Key, ProjectID: value.ProjectID, Title: value.Title, Description: value.Description, Status: value.Status, Priority: value.Priority, CurrentBranch: value.CurrentBranch, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
	if value.AssigneeType != nil && value.AssigneeID != nil {
		out.AssignedTo = &AssigneeDTO{Type: *value.AssigneeType, ID: *value.AssigneeID}
		if value.AssigneeName != nil {
			out.AssignedTo.Name = *value.AssigneeName
		}
	}
	if value.CreatedByType != nil && value.CreatedByID != nil {
		out.CreatedBy = &ActorDTO{Type: *value.CreatedByType, ID: *value.CreatedByID}
		if value.CreatedByName != nil {
			out.CreatedBy.Name = *value.CreatedByName
		}
	}
	if value.LastEvent != nil {
		out.LastEvent = &EventSummaryDTO{ID: value.LastEvent.ID, Type: value.LastEvent.Type, OccurredAt: value.LastEvent.OccurredAt}
	}
	return out
}

func relationshipDTO(value store.IssueRelationship, keys map[string]string) RelationshipDTO {
	return RelationshipDTO{ID: value.ID, ProjectID: value.ProjectID, SourceIssueID: keys[value.SourceIssueID], TargetIssueID: keys[value.TargetIssueID], Type: RelationshipType(value.Type), CreatedAt: value.CreatedAt}
}

func runDTO(value store.Run, keys map[string]string) RunDTO {
	return RunDTO{ID: value.ID, ProjectID: value.ProjectID, IssueID: keys[value.IssueID], AgentID: value.AgentID, Attempt: value.Attempt, Status: value.Status, QueueReason: value.QueueReason, FailureReason: value.FailureReason, CurrentBranch: value.CurrentBranch, CreatedAt: value.CreatedAt, StartedAt: value.StartedAt, CompletedAt: value.CompletedAt, UpdatedAt: value.UpdatedAt}
}

func questionDTO(value store.Question, keys map[string]string) QuestionDTO {
	return QuestionDTO{ID: value.ID, ProjectID: value.ProjectID, IssueID: keys[value.IssueID], RunID: value.RunID, Prompt: value.Prompt, Kind: value.Kind, Options: jsonValue(value.Options), Recommendation: value.Recommendation, Custom: value.Custom, Blocking: value.Blocking, Status: value.Status, CreatedAt: value.CreatedAt, AnsweredAt: value.AnsweredAt}
}

func decisionDTO(value store.Decision, keys map[string]string) DecisionDTO {
	out := DecisionDTO{ID: value.ID, ProjectID: value.ProjectID, RunID: value.RunID, QuestionID: value.QuestionID, Kind: value.Kind, Outcome: value.Outcome, ActorType: value.ActorType, ActorID: value.ActorID, SafeDetails: jsonValue(value.SafeDetails), CreatedAt: value.CreatedAt}
	if value.IssueID != nil {
		if key := keys[*value.IssueID]; key != "" {
			out.IssueID = &key
		}
	}
	return out
}

func reviewDTO(value store.Review, keys map[string]string) ReviewDTO {
	return ReviewDTO{ID: value.ID, ProjectID: value.ProjectID, IssueID: keys[value.IssueID], RunID: value.RunID, Status: value.Status, DecisionID: value.DecisionID, BaseRevision: value.BaseRevision, ReviewRevision: value.ReviewRevision, RequestedAt: value.RequestedAt, DecidedAt: value.DecidedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func (s *Server) issueKeys(ctx context.Context, actor app.AuthenticatedUser, projectID string) (map[string]string, error) {
	issues, err := s.services.ProjectAccess.ListIssues(ctx, actor, projectID)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]string, len(issues))
	for _, issue := range issues {
		keys[issue.ID] = issue.Key
	}
	return keys, nil
}

func (s *Server) resolveIssue(ctx context.Context, actor app.AuthenticatedUser, projectID, issueKey string) (string, error) {
	return s.services.ProjectAccess.ResolveIssueUUID(ctx, actor, projectID, issueKey)
}

func jsonValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func valueFromJSONMarshal(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return jsonValue(raw)
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
