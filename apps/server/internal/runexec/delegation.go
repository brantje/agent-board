package runexec

import (
	"context"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationRequester struct {
	service      *app.Service
	delegations store.DelegationStore
	safe         executioncontext.SafeContext
}

func newDelegationRequester(candidate any, events *evidence.Recorder, safe executioncontext.SafeContext) engine.DelegationRequester {
	controlPlane, ok := candidate.(store.ControlPlaneStore)
	if !ok {
		return nil
	}
	service := app.New(controlPlane)
	if events != nil {
		service.SetEventRecorder(events)
	}
	delegations, _ := candidate.(store.DelegationStore)
	return &delegationRequester{service: service, delegations: delegations, safe: safe}
}

func normalizeDelegationRequest(request engine.DelegationRequest) (engine.DelegationRequest, error) {
	request.TargetAgentID = strings.TrimSpace(request.TargetAgentID)
	request.Task = strings.TrimSpace(request.Task)
	request.RequestKey = strings.TrimSpace(request.RequestKey)
	if request.TargetAgentID == "" || request.Task == "" || request.RequestKey == "" {
		return engine.DelegationRequest{}, fmt.Errorf("run execution: delegation target, task and request key are required")
	}
	return request, nil
}

func (r *delegationRequester) Delegate(ctx context.Context, request engine.DelegationRequest) (engine.Delegation, error) {
	if r == nil || r.service == nil {
		return engine.Delegation{}, fmt.Errorf("run execution: delegation capability is unavailable")
	}
	request, err := normalizeDelegationRequest(request)
	if err != nil {
		return engine.Delegation{}, err
	}
	result, err := r.service.RequestDelegation(ctx, r.safe.Project.ID, r.safe.Run.ID, app.DelegationRequest{
		TargetAgentID: request.TargetAgentID,
		Task:          request.Task,
		RequestKey:    request.RequestKey,
	})
	if err != nil {
		return engine.Delegation{}, err
	}
	return engine.Delegation{ID: result.Delegation.ID, RunID: result.DelegatedRun.ID}, nil
}

func (r *delegationRequester) ResolveAcceptedDelegation(ctx context.Context, request engine.DelegationRequest) (engine.Delegation, bool, error) {
	if r == nil || r.delegations == nil {
		return engine.Delegation{}, false, fmt.Errorf("run execution: delegation recovery lookup is unavailable")
	}
	request, err := normalizeDelegationRequest(request)
	if err != nil {
		return engine.Delegation{}, false, err
	}
	values, err := r.delegations.ListDelegationsByParentRun(ctx, r.safe.Project.ID, r.safe.Run.ID)
	if err != nil {
		return engine.Delegation{}, false, err
	}
	for _, value := range values {
		if value.RequestKey != request.RequestKey {
			continue
		}
		if value.Outcome != nil {
			return engine.Delegation{}, false, nil
		}
		// The durable request key proves that this logical delegation was
		// already accepted. Replay only now so the canonical idempotency,
		// redaction, target and task checks validate the native history without
		// permitting an error-only history record to create work.
		delegation, err := r.Delegate(ctx, request)
		if err != nil {
			return engine.Delegation{}, false, err
		}
		return delegation, true, nil
	}
	return engine.Delegation{}, false, nil
}
