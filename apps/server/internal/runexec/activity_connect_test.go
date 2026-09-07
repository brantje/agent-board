package runexec

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProcessLauncherRecordActivityPersistsOnlyCanonicalEngineEvents(t *testing.T) {
	events := &questionEventStore{}
	recorder, err := evidence.NewRecorder(events, nil)
	if err != nil {
		t.Fatal(err)
	}
	launcher := &processLauncher{
		events:            recorder,
		safe:              interactiveSafeContext(),
		runtimeInstanceID: "runtime-instance-1",
	}
	activity := engine.ActivityEvent{Type: "agent.message", Payload: map[string]any{"message": "visible progress", "source": "opencode"}}
	if err := launcher.RecordActivity(context.Background(), activity); err != nil {
		t.Fatalf("RecordActivity() error=%v", err)
	}
	if len(events.events) != 1 || events.events[0].Type != "agent.message" || events.events[0].RunID == nil || *events.events[0].RunID != "run-1" {
		t.Fatalf("events=%+v", events.events)
	}
	if err := launcher.RecordActivity(context.Background(), engine.ActivityEvent{Type: "private.reasoning"}); err == nil {
		t.Fatal("unsupported Engine activity type unexpectedly persisted")
	}
	var unavailable *processLauncher
	if err := unavailable.RecordActivity(context.Background(), activity); err == nil {
		t.Fatal("nil activity sink unexpectedly accepted event")
	}
}

type launcherDialClient struct {
	*launcherClient
	conn      net.Conn
	sessionID string
	network   string
	address   string
}

func (c *launcherDialClient) DialSession(_ context.Context, sessionID, network, address string) (net.Conn, error) {
	c.sessionID = sessionID
	c.network = network
	c.address = address
	return c.conn, nil
}

func TestCapturingProcessDialsThroughItsOwningExecutionSession(t *testing.T) {
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
		instance: store.RuntimeInstance{ID: "runtime-instance", ProjectID: safe.Project.ID, WorkspaceID: safe.Workspace.ID, RuntimeID: safe.Runtime.ID, Status: "RUNNING"},
	}
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = local.Close(); _ = remote.Close() })
	client := &launcherDialClient{launcherClient: newLauncherClient("", "", 0, nil), conn: local}
	transportSessions, err := app.NewExecutionSessionService(sessionStore, launcherRunnerManager{client: client})
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
	process, err := launcher.Start(t.Context(), engine.ProcessRequest{Kind: "tool", Name: "opencode-server", Command: []string{"opencode", "serve"}, CWD: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	connector, ok := process.(engine.SessionConnector)
	if !ok {
		t.Fatal("capturing process does not expose session connector")
	}
	conn, err := connector.DialContext(t.Context(), "tcp", "127.0.0.1:4096")
	if err != nil {
		t.Fatalf("DialContext() error=%v", err)
	}
	if client.sessionID != process.ID() || client.network != "tcp" || client.address != "127.0.0.1:4096" {
		t.Fatalf("dial forwarded session=%q network=%q address=%q process=%q", client.sessionID, client.network, client.address, process.ID())
	}

	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := remote.Write([]byte("ok"))
		writeDone <- writeErr
	}()
	buffer := make([]byte, 2)
	if _, err := io.ReadFull(conn, buffer); err != nil || string(buffer) != "ok" {
		t.Fatalf("session conn read=%q err=%v", buffer, err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("remote write error=%v", err)
	}
	_ = conn.Close()

	if _, err := process.Wait(t.Context()); err != nil {
		t.Fatalf("Wait() error=%v", err)
	}
}

func TestCapturingProcessRejectsUnavailableSessionConnector(t *testing.T) {
	var process *capturingProcess
	if _, err := process.DialContext(t.Context(), "tcp", "127.0.0.1:4096"); err == nil {
		t.Fatal("nil capturing process unexpectedly dialed")
	}
}

var _ runner.SessionDialer = (*launcherDialClient)(nil)
