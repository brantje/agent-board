package app

import (
	"context"
	"errors"
	"io"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// ListProjectMembers exposes the safe effective human Project directory. It is
// distinct from direct access-grant administration and intentionally contains
// no authentication/session state.
func (s *ProjectAccessService) ListProjectMembers(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.ProjectMember, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.store.ListProjectMembers(ctx, projectID)
}

func (s *ProjectAccessService) ListAgents(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.Agent, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	scope := projectID
	return s.controlPlane.ListAgents(ctx, &scope)
}

func (s *ProjectAccessService) GetAgent(ctx context.Context, actor AuthenticatedUser, projectID, agentID string) (store.Agent, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return store.Agent{}, err
	}
	scope := projectID
	return s.controlPlane.GetAgent(ctx, &scope, agentID)
}

func (s *ProjectAccessService) ListIssueRelationships(ctx context.Context, actor AuthenticatedUser, projectID, issueID string) ([]store.IssueRelationship, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.controlPlane.ListIssueRelationships(ctx, projectID, issueID)
}

func (s *ProjectAccessService) CreateIssueRelationship(ctx context.Context, actor AuthenticatedUser, input store.IssueRelationship) (store.IssueRelationship, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, input.ProjectID); err != nil {
		return store.IssueRelationship{}, err
	}
	return s.controlPlane.CreateIssueRelationship(ctx, input)
}

func (s *ProjectAccessService) DeleteIssueRelationship(ctx context.Context, actor AuthenticatedUser, projectID, issueID, relationshipID string) error {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, projectID); err != nil {
		return err
	}
	return s.controlPlane.DeleteIssueRelationship(ctx, projectID, issueID, relationshipID)
}

func (s *ProjectAccessService) GetIssueExecutionState(ctx context.Context, actor AuthenticatedUser, projectID, issueID string) (store.IssueExecutionState, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return store.IssueExecutionState{}, err
	}
	return s.controlPlane.GetIssueExecutionState(ctx, projectID, issueID)
}

func (s *ProjectAccessService) InspectRun(ctx context.Context, actor AuthenticatedUser, evidence *RunEvidenceService, projectID, runID string) (RunEvidence, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return RunEvidence{}, err
	}
	if evidence == nil {
		return RunEvidence{}, errors.New("run evidence service is unavailable")
	}
	return evidence.Inspect(ctx, projectID, runID)
}

func (s *ProjectAccessService) OpenRunRawOutput(ctx context.Context, actor AuthenticatedUser, evidence *RunEvidenceService, projectID, runID, chunkID string) (store.RawOutputChunk, io.ReadCloser, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return store.RawOutputChunk{}, nil, err
	}
	if evidence == nil {
		return store.RawOutputChunk{}, nil, errors.New("run evidence service is unavailable")
	}
	return evidence.OpenRawOutput(ctx, projectID, runID, chunkID)
}
