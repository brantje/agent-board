package runexec

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
)

func TestRegistryConnectorRejectsMissingRegistryAndDisconnectedRunner(t *testing.T) {
	if NewRegistryConnector(nil) != nil {
		t.Fatal("nil registry produced a connector")
	}

	connector := NewRegistryConnector(runner.NewRegistry(nil, nil))
	if connector == nil {
		t.Fatal("registry did not produce a connector")
	}
	if _, err := connector.Connect(t.Context(), "project-1", "runner-1"); !errors.Is(err, runner.ErrDisconnected) {
		t.Fatalf("Connect() error=%v", err)
	}
}
