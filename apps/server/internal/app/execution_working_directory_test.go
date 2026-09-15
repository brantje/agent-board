package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestExecutionProcessSurfacesRunnerWorkingDirectory(t *testing.T) {
	const hostDir = "/tmp/external-runner-workspaces/session-1"
	storeFake := newBranchExecutionStore()
	transport := newFakeExecutionTransport("session-1")
	transport.workingDir = hostDir
	service := newBranchExecutionService(t, storeFake, transport, nil)
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}, CWD: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if got := process.WorkingDirectory(); got != hostDir {
		t.Fatalf("WorkingDirectory()=%q want %q", got, hostDir)
	}
	close(transport.resultCh)
}

func TestExecutionProcessWorkingDirectoryWithoutProvider(t *testing.T) {
	transport := newCancelBranchTransport("session-1")
	_, process := startCancelBranchProcess(t, transport)
	if got := process.WorkingDirectory(); got != "" {
		t.Fatalf("WorkingDirectory()=%q want empty without provider", got)
	}
	transport.finish()
}

func TestAuthorizedExecutionProcessSurfacesRunnerWorkingDirectory(t *testing.T) {
	const hostDir = "/tmp/external-runner-workspaces/session-auth"
	lowLevel, _, transport := executionServiceFixture(t)
	transport.workingDir = hostDir
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{prepared: executioncontext.Prepared{}})
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.Start(t.Context(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := process.WorkingDirectory(); got != hostDir {
		t.Fatalf("WorkingDirectory()=%q want %q", got, hostDir)
	}
	var nilProcess *AuthorizedExecutionProcess
	if got := nilProcess.WorkingDirectory(); got != "" {
		t.Fatalf("nil WorkingDirectory()=%q want empty", got)
	}
	close(transport.resultCh)
}
