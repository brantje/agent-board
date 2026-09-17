package app

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

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

func (s *Service) MarkDelegationWorkspaceHandoffReady(ctx context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("delegation service is unavailable")
	}
	handoffs, ok := any(s.store).(store.DelegationWorkspaceHandoffStore)
	if !ok {
		return fmt.Errorf("delegation workspace handoff store is unavailable")
	}
	return translateStoreError(handoffs.MarkDelegationWorkspaceHandoffReady(ctx, projectID, parentRunID, delegationID, delegatedRunID), "delegation workspace handoff")
}

func (s *Service) GetDelegationByRun(ctx context.Context, projectID, runID string) (store.Delegation, error) {
	delegations, ok := any(s.store).(store.DelegationStore)
	if !ok {
		return store.Delegation{}, fmt.Errorf("delegation store is unavailable")
	}
	value, err := delegations.GetDelegationByRun(ctx, projectID, runID)
	return value, translateStoreError(err, "delegation")
}

func (s *Service) ListDelegationsByParentRun(ctx context.Context, projectID, parentRunID string) ([]store.Delegation, error) {
	delegations, ok := any(s.store).(store.DelegationStore)
	if !ok {
		return nil, fmt.Errorf("delegation store is unavailable")
	}
	values, err := delegations.ListDelegationsByParentRun(ctx, projectID, parentRunID)
	return values, translateStoreError(err, "delegation")
}
