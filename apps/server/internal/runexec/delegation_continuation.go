package runexec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (p *Processor) loadDelegationContinuation(ctx context.Context, claim *store.SchedulerAdmission, run store.Run) (*engine.DelegationContinuation, error) {
	if claim == nil || claim.Job.Kind != "RESUME" {
		return nil, nil
	}
	delegations, ok := p.store.(store.DelegationContinuationStore)
	if !ok {
		return nil, nil
	}
	delegation, err := delegations.GetDelegationByContinuationJob(ctx, run.ProjectID, run.ID, claim.Job.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if delegation.Outcome == nil || delegation.ResultSummary == nil || delegation.WorkspaceChangesAccepted == nil || delegation.CompletedAt == nil || delegation.DelegatedRunID == "" || strings.TrimSpace(delegation.Task) == "" {
		return nil, fmt.Errorf("run execution: delegation continuation is incomplete")
	}
	switch *delegation.Outcome {
	case store.DelegationOutcomeSucceeded, store.DelegationOutcomeFailed, store.DelegationOutcomeCancelled:
	default:
		return nil, fmt.Errorf("run execution: delegation continuation has invalid outcome %q", *delegation.Outcome)
	}
	revisions, ok := p.store.(store.WorkspaceRevisionStore)
	if !ok {
		return nil, fmt.Errorf("run execution: Workspace revision store is unavailable for delegation continuation")
	}
	revision, err := revisions.GetWorkspaceCurrentRevision(ctx, run.ProjectID, run.WorkspaceID)
	if err != nil {
		return nil, err
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return nil, fmt.Errorf("run execution: current Workspace revision is unavailable for delegation continuation")
	}
	resultEventID := ""
	if delegation.ResultEventID != nil {
		resultEventID = *delegation.ResultEventID
	}
	return &engine.DelegationContinuation{
		DelegationID:             delegation.ID,
		TargetAgentID:            delegation.TargetAgentID,
		Task:                     delegation.Task,
		Outcome:                  *delegation.Outcome,
		ResultSummary:            *delegation.ResultSummary,
		DelegatedRunID:           delegation.DelegatedRunID,
		ResultEventID:            resultEventID,
		WorkspaceChangesAccepted: *delegation.WorkspaceChangesAccepted,
		WorkspaceRevision:        revision,
	}, nil
}
