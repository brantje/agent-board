package runexec

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func createExternalRunnerCredential(ctx context.Context, control *app.Service) (store.Runner, string, error) {
	runner, token, err := control.Runners.Create(ctx, "External integration host")
	if err != nil {
		return store.Runner{}, "", err
	}
	return runner, token, nil
}
