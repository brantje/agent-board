package runexec

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/runner"
)

type registryConnector struct {
	registry *runner.Registry
}

func NewRegistryConnector(registry *runner.Registry) RunnerConnector {
	if registry == nil {
		return nil
	}
	return &registryConnector{registry: registry}
}

func (c *registryConnector) Connect(ctx context.Context, projectID, runnerID string) (runnerClient, error) {
	client, err := c.registry.Connect(ctx, projectID, runnerID)
	if err != nil {
		return nil, err
	}
	if conn, ok := client.(*runner.Connection); ok {
		return conn, nil
	}
	return nil, runner.ErrDisconnected
}
