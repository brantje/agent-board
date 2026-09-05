package app

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Service) AssignIssue(ctx context.Context, projectID, issueID, agentID string) (store.Issue, store.Run, error) {
	issue, err := s.GetIssue(ctx, projectID, issueID)
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}
	if issue.Status == "DONE" {
		return store.Issue{}, store.Run{}, NewError("issue_done", "done issue cannot start a run", store.ErrConflict)
	}

	scope := &projectID
	agent, err := s.GetAgent(ctx, scope, agentID)
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}
	if agent.State != "ENABLED" {
		return store.Issue{}, store.Run{}, NewError("agent_unavailable", "agent is not runnable", store.ErrConflict)
	}

	executor, err := s.GetExecutorProfile(ctx, scope, agent.ExecutorProfileID)
	if err != nil {
		return store.Issue{}, store.Run{}, executionConfigError(err)
	}
	if !executor.Enabled {
		return store.Issue{}, store.Run{}, executionConfigError(nil)
	}

	model, err := s.GetModelProfile(ctx, scope, executor.ModelProfileID)
	if err != nil {
		return store.Issue{}, store.Run{}, executionConfigError(err)
	}
	if !model.Enabled {
		return store.Issue{}, store.Run{}, executionConfigError(nil)
	}

	provider, err := s.GetProvider(ctx, model.ProviderID)
	if err != nil {
		return store.Issue{}, store.Run{}, executionConfigError(err)
	}
	if !provider.Enabled || provider.HealthStatus == "UNHEALTHY" {
		return store.Issue{}, store.Run{}, executionConfigError(nil)
	}

	runtime, err := s.GetRuntime(ctx, scope, executor.RuntimeID)
	if err != nil {
		return store.Issue{}, store.Run{}, executionConfigError(err)
	}
	if !runtime.Enabled || runtime.HealthStatus == "UNHEALTHY" || runtime.Kind != "docker" || strings.TrimSpace(runtime.Image) == "" {
		return store.Issue{}, store.Run{}, executionConfigError(nil)
	}

	assigned, run, err := s.store.AssignIssue(ctx, projectID, issueID, agentID)
	if err != nil {
		return store.Issue{}, store.Run{}, translateStoreError(err, "issue")
	}
	return assigned, run, nil
}

func executionConfigError(err error) error {
	return NewError("execution_configuration_invalid", "agent execution configuration is not runnable", err)
}
