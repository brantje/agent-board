package runexec

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func createExternalRunnerCredential(t *testing.T, ctx context.Context, service *app.Service, name string) (store.Runner, string) {
	t.Helper()
	runner, token, err := service.Runners.Create(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	return runner, token
}
