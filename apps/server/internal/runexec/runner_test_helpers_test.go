package runexec

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func createExternalRunnerCredential(ctx context.Context, control *app.Service) (store.Runner, string, error) {
	created, registrationToken, err := control.Runners.Create(ctx)
	if err != nil {
		return store.Runner{}, "", err
	}
	runner, token, err := control.Runners.Register(ctx, registrationToken, "External integration host "+created.ID)
	if err != nil {
		return store.Runner{}, "", err
	}
	return runner, token, nil
}
