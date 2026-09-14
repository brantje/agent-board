package app

import (
	"context"
	"log/slog"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Service) StartIssueRun(ctx context.Context, projectID, issueID string) (store.Run, error) {
	execution, ok := s.store.(store.IssueExecutionStore)
	if !ok {
		return store.Run{}, invalid("Issue execution unavailable")
	}
	run, event, err := execution.StartIssueRun(ctx, projectID, issueID)
	if err != nil {
		return store.Run{}, translateStoreError(err, "issue")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	if event.ID != "" {
		publishPersistedEvents(ctx, publisher, []store.Event{event})
	}
	return run, nil
}

func (s *ProjectAccessService) StartIssueRun(ctx context.Context, actor AuthenticatedUser, projectID, issueID string) (store.Run, error) {
	if _, err := s.RequireRole(ctx, actor, projectID, store.ProjectRoleMember); err != nil {
		return store.Run{}, err
	}
	return s.controlPlane.StartIssueRun(ctx, projectID, issueID)
}

// Recovery derives work from current assignment/configuration. No pending flag,
// Runner polling, scheduler admission or alternative execution path is involved.
func (s *Service) ReconcileIssueExecution(ctx context.Context, filter store.IssueExecutionFilter) error {
	execution, ok := s.store.(store.IssueExecutionStore)
	if !ok {
		return nil
	}
	events, err := execution.ReconcileIssueExecution(ctx, filter)
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, events)
	return err
}

func (s *Service) reconcileExecutionConfiguration(ctx context.Context, filter store.IssueExecutionFilter) {
	if err := s.ReconcileIssueExecution(ctx, filter); err != nil {
		slog.ErrorContext(ctx, "reconcile execution configuration", "agent_id", filter.AgentID, "model_profile_id", filter.ModelProfileID, "provider_id", filter.ProviderID, "error", err)
	}
}
