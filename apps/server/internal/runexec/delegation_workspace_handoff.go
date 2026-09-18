package runexec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (p *Processor) finishDelegationWorkspaceHandoff(ctx context.Context, safe executioncontext.SafeContext, delegation engine.Delegation, runtimeInstanceID *string) (scheduler.Result, error) {
	delegation.ID = strings.TrimSpace(delegation.ID)
	delegation.RunID = strings.TrimSpace(delegation.RunID)
	if delegation.ID == "" || delegation.RunID == "" {
		return p.failExecution(ctx, safe, fmt.Errorf("run execution: delegation handoff identity is incomplete"), runtimeInstanceID)
	}
	handoffs, ok := any(p.store).(store.DelegationWorkspaceHandoffStore)
	if !ok {
		return p.failExecution(ctx, safe, fmt.Errorf("run execution: delegation Workspace handoff store is unavailable"), runtimeInstanceID)
	}
	handoffCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err := handoffs.MarkDelegationWorkspaceHandoffReady(handoffCtx, safe.Project.ID, safe.Run.ID, delegation.ID, delegation.RunID); err != nil {
		return p.failExecution(handoffCtx, safe, fmt.Errorf("run execution: mark delegation Workspace handoff ready: %w", err), runtimeInstanceID)
	}
	if err := p.record(handoffCtx, safe, "run.paused", map[string]any{
		"reason":         "delegation_handoff",
		"delegationId":   delegation.ID,
		"delegatedRunId": delegation.RunID,
	}, runtimeInstanceID, nil); err != nil {
		return scheduler.Result{}, err
	}
	return scheduler.Result{RunStatus: "PAUSED"}, nil
}

func (p *Processor) failExecution(ctx context.Context, safe executioncontext.SafeContext, cause error, runtimeInstanceID *string) (scheduler.Result, error) {
	reason := safeFailure(cause)
	result := scheduler.Result{RunStatus: "FAILED", FailureReason: &reason}
	checked, err := p.checkedExecutionResult(ctx, store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID}, result)
	if err != nil {
		return scheduler.Result{}, errors.Join(cause, err)
	}
	_ = p.record(ctx, safe, "run.failed", map[string]any{"reason": reason}, runtimeInstanceID, nil)
	return checked, nil
}
