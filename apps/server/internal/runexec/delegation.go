package runexec

import (
	"context"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationRequester struct {
	service *app.Service
	safe    executioncontext.SafeContext
}

func newDelegationRequester(candidate any, safe executioncontext.SafeContext) engine.DelegationRequester {
	controlPlane, ok := candidate.(store.ControlPlaneStore)
	if !ok {
		return nil
	}
	return &delegationRequester{service: app.New(controlPlane), safe: safe}
}

func (r *delegationRequester) Delegate(ctx context.Context, request engine.DelegationRequest) (engine.Delegation, error) {
	if r == nil || r.service == nil {
		return engine.Delegation{}, fmt.Errorf("run execution: delegation capability is unavailable")
	}
	request.TargetAgentID = strings.TrimSpace(request.TargetAgentID)
	request.Task = strings.TrimSpace(request.Task)
	request.RequestKey = strings.TrimSpace(request.RequestKey)
	if request.TargetAgentID == "" || request.Task == "" || request.RequestKey == "" {
		return engine.Delegation{}, fmt.Errorf("run execution: delegation target, task and request key are required")
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
