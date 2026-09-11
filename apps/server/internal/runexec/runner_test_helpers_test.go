package runexec

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func createExternalRunnerCredential(ctx context.Context, control *app.Service) (store.Runner, string, error) {
	return createExternalRunnerCredentialNamed(ctx, control, "External integration host")
}

func createExternalRunnerCredentialNamed(ctx context.Context, control *app.Service, name string) (store.Runner, string, error) {
	_, registrationToken, err := control.Runners.Create(ctx)
	if err != nil {
		return store.Runner{}, "", err
	}
	runner, token, err := control.Runners.Register(ctx, registrationToken, name)
	if err != nil {
		return store.Runner{}, "", err
	}
	return runner, token, nil
}
