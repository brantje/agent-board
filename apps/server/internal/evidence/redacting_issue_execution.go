package evidence

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) issueExecutionStore() (store.IssueExecutionStore, error) {
	base, ok := s.ControlPlaneStore.(store.IssueExecutionStore)
	if !ok {
		return nil, fmt.Errorf("redacting store base does not support Issue execution")
	}
	return base, nil
}

func (s *RedactingStore) issueExecutionReadinessStore() (store.IssueExecutionReadinessStore, error) {
	base, ok := s.ControlPlaneStore.(store.IssueExecutionReadinessStore)
	if !ok {
		return nil, fmt.Errorf("redacting store base does not support Issue execution readiness")
	}
	return base, nil
}

func (s *RedactingStore) issueExecutionStateStore() (store.IssueExecutionStateStore, error) {
	base, ok := s.ControlPlaneStore.(store.IssueExecutionStateStore)
	if !ok {
		return nil, fmt.Errorf("redacting store base does not support Issue execution state")
	}
	return base, nil
}

func (s *RedactingStore) StartIssueRun(ctx context.Context, projectID, issueID string) (store.Run, store.Event, error) {
	base, err := s.issueExecutionStore()
	if err != nil {
		return store.Run{}, store.Event{}, err
	}
	return base.StartIssueRun(ctx, projectID, issueID)
}

func (s *RedactingStore) ReconcileIssueExecution(ctx context.Context, filter store.IssueExecutionFilter) ([]store.Event, error) {
	base, err := s.issueExecutionStore()
	if err != nil {
		return nil, err
	}
	return base.ReconcileIssueExecution(ctx, filter)
}

func (s *RedactingStore) RunnableIssueExecutionScopes(ctx context.Context, filter store.IssueExecutionFilter) ([]store.IssueExecutionScope, error) {
	base, err := s.issueExecutionReadinessStore()
	if err != nil {
		return nil, err
	}
	return base.RunnableIssueExecutionScopes(ctx, filter)
}

func (s *RedactingStore) GetIssueExecutionState(ctx context.Context, projectID, issueID string) (store.IssueExecutionState, error) {
	base, err := s.issueExecutionStateStore()
	if err != nil {
		return store.IssueExecutionState{}, err
	}
	return base.GetIssueExecutionState(ctx, projectID, issueID)
}

var _ store.IssueExecutionStore = (*RedactingStore)(nil)
var _ store.IssueExecutionReadinessStore = (*RedactingStore)(nil)
var _ store.IssueExecutionStateStore = (*RedactingStore)(nil)
