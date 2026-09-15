package runexec

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCapturingProcessSurfacesRunnerWorkingDirectory(t *testing.T) {
	const hostDir = "/tmp/external-runner-workspaces/session-capture"
	safe := processTestSafeContext(t.TempDir())
	evidenceStore := &processTestStore{}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(evidenceStore, blobs, 16)
	if err != nil {
		t.Fatal(err)
	}
	sessionStore := &launcherSessionStore{
		run:      store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, WorkspaceID: safe.Workspace.ID},
		instance: store.RuntimeInstance{ID: "runtime-instance", ProjectID: safe.Project.ID, WorkspaceID: safe.Workspace.ID, Status: "RUNNING"},
	}
	client := newLauncherClient("", "", 0, nil)
	client.workingDir = hostDir
	transportSessions, err := newLauncherExecutionSessionService(sessionStore, client)
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := app.NewAuthorizedExecutionSessionService(transportSessions, launcherPreparer{runtimeID: safe.Runtime.ID})
	if err != nil {
		t.Fatal(err)
	}
	launcher := &processLauncher{
		sessions:          authorized,
		events:            recorder,
		output:            output,
		safe:              safe,
		runtimeInstanceID: "runtime-instance",
		scope:             evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
	}
	process, err := launcher.Start(t.Context(), engine.ProcessRequest{Kind: "tool", Name: "fixture", Command: []string{"fixture"}, CWD: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := process.(engine.WorkingDirectoryProvider)
	if !ok {
		t.Fatal("capturing process does not implement WorkingDirectoryProvider")
	}
	if got := provider.WorkingDirectory(); got != hostDir {
		t.Fatalf("WorkingDirectory()=%q want %q", got, hostDir)
	}
	var nilProcess *capturingProcess
	if got := nilProcess.WorkingDirectory(); got != "" {
		t.Fatalf("nil WorkingDirectory()=%q want empty", got)
	}
	if got := (&capturingProcess{}).WorkingDirectory(); got != "" {
		t.Fatalf("empty process WorkingDirectory()=%q want empty", got)
	}
	_ = process.Terminate(t.Context())
	_, _ = process.Wait(t.Context())
}
