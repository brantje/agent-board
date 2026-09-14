package app

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) SetIssueAssignee(ctx context.Context, projectID, issueID string, target *store.Assignee, actor json.RawMessage) (store.Issue, error) {
	if target != nil {
		var id pgtype.UUID
		if err := id.Scan(target.ID); err != nil || (target.Type != "USER" && target.Type != "AGENT") {
			return store.Issue{}, NewError("invalid_argument", "assignee must have a valid type and UUID", store.ErrInvalidArgument)
		}
	}
	assignmentStore := s.assignmentStore
	if assignmentStore == nil {
		return store.Issue{}, NewError("assignment_unavailable", "assignment store unavailable", store.ErrInvalidArgument)
	}
	result, err := assignmentStore.SetIssueAssignee(ctx, projectID, issueID, target, actor)
	if err != nil {
		return store.Issue{}, translateStoreError(err, "assignee")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	return result.Issue, nil
}

func (s *Service) ListIssueAssignees(ctx context.Context, projectID string) ([]store.Assignee, error) {
	assignmentStore := s.assignmentStore
	if assignmentStore == nil {
		return nil, NewError("assignment_unavailable", "assignment store unavailable", store.ErrInvalidArgument)
	}
	result, err := assignmentStore.ListIssueAssignees(ctx, projectID)
	return result, translateStoreError(err, "project")
}

func (s *ProjectAccessService) SetIssueAssignee(ctx context.Context, actor AuthenticatedUser, projectID, issueID string, target *store.Assignee) (store.Issue, error) {
	if _, err := s.RequireRole(ctx, actor, projectID, store.ProjectRoleMember); err != nil {
		return store.Issue{}, err
	}
	encoded, _ := json.Marshal(map[string]string{"type": "HUMAN", "id": actor.ID})
	return s.controlPlane.SetIssueAssignee(ctx, projectID, issueID, target, encoded)
}

func (s *ProjectAccessService) ListIssueAssignees(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.Assignee, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.controlPlane.ListIssueAssignees(ctx, projectID)
}
