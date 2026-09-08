package app

import (
	"context"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func validIssueRelationshipType(value string) bool {
	switch value {
	case "blocks", "depends_on", "related_to", "duplicates":
		return true
	default:
		return false
	}
}

func (s *Service) ListIssueRelationships(ctx context.Context, projectID, sourceIssueID string) ([]store.IssueRelationship, error) {
	if _, err := s.GetIssue(ctx, projectID, sourceIssueID); err != nil {
		return nil, err
	}
	values, err := s.store.ListIssueRelationships(ctx, projectID, sourceIssueID)
	if err != nil {
		return nil, translateStoreError(err, "issue_relationship")
	}
	return values, nil
}

func (s *Service) CreateIssueRelationship(ctx context.Context, input store.IssueRelationship) (store.IssueRelationship, error) {
	if _, err := s.GetIssue(ctx, input.ProjectID, input.SourceIssueID); err != nil {
		return store.IssueRelationship{}, err
	}
	if input.SourceIssueID == input.TargetIssueID {
		return store.IssueRelationship{}, NewError("issue_relationship_self_reference", "an issue cannot relate to itself", store.ErrInvalidArgument)
	}
	if !validIssueRelationshipType(input.Type) {
		return store.IssueRelationship{}, NewError("invalid_argument", "invalid issue relationship type", store.ErrInvalidArgument)
	}
	if _, err := s.GetIssue(ctx, input.ProjectID, input.TargetIssueID); err != nil {
		return store.IssueRelationship{}, NewError("issue_relationship_target_not_found", "relationship target issue not found", err)
	}
	value, err := s.store.CreateIssueRelationship(ctx, input)
	if err == nil {
		return value, nil
	}
	if errors.Is(err, store.ErrConflict) {
		return store.IssueRelationship{}, NewError("issue_relationship_exists", "issue relationship already exists", err)
	}
	if errors.Is(err, store.ErrNotFound) {
		return store.IssueRelationship{}, NewError("issue_relationship_target_not_found", "relationship target issue not found", err)
	}
	return store.IssueRelationship{}, translateStoreError(err, "issue_relationship")
}

func (s *Service) DeleteIssueRelationship(ctx context.Context, projectID, sourceIssueID, relationshipID string) error {
	if _, err := s.GetIssue(ctx, projectID, sourceIssueID); err != nil {
		return err
	}
	if err := s.store.DeleteIssueRelationship(ctx, projectID, sourceIssueID, relationshipID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NewError("issue_relationship_not_found", "issue relationship not found", err)
		}
		return translateStoreError(err, "issue_relationship")
	}
	return nil
}
