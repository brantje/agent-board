package opencode

import (
	"context"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type unusedLauncher struct{}

func (unusedLauncher) Start(context.Context, engine.ProcessRequest) (engine.Process, error) {
	panic("Start must not be called while validating execution capabilities")
}

func TestExecuteRequiresServerOwnedCapabilities(t *testing.T) {
	adapter := New()

	if _, err := adapter.Execute(context.Background(), engine.Request{}); err == nil || !strings.Contains(err.Error(), "process launcher is required") {
		t.Fatalf("Execute without launcher error=%v", err)
	}

	if _, err := adapter.Execute(context.Background(), engine.Request{Launcher: unusedLauncher{}}); err == nil || !strings.Contains(err.Error(), "interactive Question capability is required") {
		t.Fatalf("Execute without interactive Questions error=%v", err)
	}
}
