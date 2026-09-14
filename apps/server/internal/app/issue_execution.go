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

type issueExecutionReadinessSnapshot map[store.IssueExecutionScope]struct{}

func (s *Service) issueExecutionReadinessSnapshot(ctx context.Context, filter store.IssueExecutionFilter) (issueExecutionReadinessSnapshot, bool) {
	readiness, ok := s.store.(store.IssueExecutionReadinessStore)
	if !ok {
		return nil, false
	}
	scopes, err := readiness.RunnableIssueExecutionScopes(ctx, filter)
	if err != nil {
		slog.ErrorContext(ctx, "snapshot execution configuration readiness", "project_id", filter.ProjectID, "agent_id", filter.AgentID, "model_profile_id", filter.ModelProfileID, "provider_id", filter.ProviderID, "error", err)
		return nil, false
	}
	snapshot := make(issueExecutionReadinessSnapshot, len(scopes))
	for _, scope := range scopes {
		snapshot[scope] = struct{}{}
	}
	return snapshot, true
}

func (s *Service) reconcileExecutionConfigurationTransition(ctx context.Context, filter store.IssueExecutionFilter, previous issueExecutionReadinessSnapshot, previousCaptured bool) {
	if !previousCaptured {
		return
	}
	current, currentCaptured := s.issueExecutionReadinessSnapshot(ctx, filter)
	if !currentCaptured {
		return
	}
	for scope := range current {
		if _, alreadyRunnable := previous[scope]; alreadyRunnable {
			continue
		}
		s.reconcileExecutionConfiguration(ctx, store.IssueExecutionFilter{ProjectID: scope.ProjectID, AgentID: scope.AgentID})
	}
}

func (s *Service) reconcileExecutionConfiguration(ctx context.Context, filter store.IssueExecutionFilter) {
	if err := s.ReconcileIssueExecution(ctx, filter); err != nil {
		slog.ErrorContext(ctx, "reconcile execution configuration", "project_id", filter.ProjectID, "agent_id", filter.AgentID, "model_profile_id", filter.ModelProfileID, "provider_id", filter.ProviderID, "error", err)
	}
}
