package app

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type DelegationInspection struct {
	store.Delegation
	ParentRunStatus    string
	DelegatedRunStatus string
	WorkspaceRevision  string
}

type DelegationRequest struct {
	TargetAgentID string
	Task          string
	RequestKey    string
}

func (s *Service) RequestDelegation(ctx context.Context, projectID, parentRunID string, input DelegationRequest) (store.RequestDelegationResult, error) {
	if s == nil || s.store == nil {
		return store.RequestDelegationResult{}, fmt.Errorf("delegation service is unavailable")
	}
	delegations, ok := any(s.store).(store.DelegationStore)
	if !ok {
		return store.RequestDelegationResult{}, fmt.Errorf("delegation store is unavailable")
	}
	result, err := delegations.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID:     projectID,
		ParentRunID:   parentRunID,
		TargetAgentID: input.TargetAgentID,
		Task:          input.Task,
		RequestKey:    input.RequestKey,
	})
	if err != nil {
		return store.RequestDelegationResult{}, translateStoreError(err, "delegation")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	return result, nil
}

func (s *Service) GetDelegationByRun(ctx context.Context, projectID, runID string) (DelegationInspection, error) {
	delegations, ok := any(s.store).(store.DelegationStore)
	if !ok {
		return DelegationInspection{}, fmt.Errorf("delegation store is unavailable")
	}
	value, err := delegations.GetDelegationByRun(ctx, projectID, runID)
	if err != nil {
		return DelegationInspection{}, translateStoreError(err, "delegation")
	}
	return s.inspectDelegation(ctx, value)
}

func (s *Service) ListDelegationsByParentRun(ctx context.Context, projectID, parentRunID string) ([]DelegationInspection, error) {
	delegations, ok := any(s.store).(store.DelegationStore)
	if !ok {
		return nil, fmt.Errorf("delegation store is unavailable")
	}
	values, err := delegations.ListDelegationsByParentRun(ctx, projectID, parentRunID)
	if err != nil {
		return nil, translateStoreError(err, "delegation")
	}
	out := make([]DelegationInspection, 0, len(values))
	for _, value := range values {
		inspection, err := s.inspectDelegation(ctx, value)
		if err != nil {
			return nil, err
		}
		out = append(out, inspection)
	}
	return out, nil
}

func (s *Service) inspectDelegation(ctx context.Context, value store.Delegation) (DelegationInspection, error) {
	parent, err := s.store.GetRun(ctx, value.ProjectID, value.ParentRunID)
	if err != nil {
		return DelegationInspection{}, translateStoreError(err, "parent Run")
	}
	child, err := s.store.GetRun(ctx, value.ProjectID, value.DelegatedRunID)
	if err != nil {
		return DelegationInspection{}, translateStoreError(err, "delegated Run")
	}
	if parent.IssueID != value.IssueID || child.IssueID != value.IssueID || parent.WorkspaceID != child.WorkspaceID {
		return DelegationInspection{}, NewError("delegation_lineage_conflict", "Delegation lineage does not match authoritative Runs", store.ErrConflict)
	}
	revisions, ok := any(s.store).(interface {
		GetWorkspaceCurrentRevision(context.Context, string, string) (string, error)
	})
	if !ok {
		return DelegationInspection{}, fmt.Errorf("Workspace revision reader is unavailable")
	}
	revision, err := revisions.GetWorkspaceCurrentRevision(ctx, value.ProjectID, child.WorkspaceID)
	if err != nil {
		return DelegationInspection{}, translateStoreError(err, "Workspace revision")
	}
	return DelegationInspection{
		Delegation: value, ParentRunStatus: parent.Status, DelegatedRunStatus: child.Status, WorkspaceRevision: revision,
	}, nil
}
