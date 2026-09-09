package runexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

type launcherSessionStore struct {
	mu       sync.Mutex
	run      store.Run
	instance store.RuntimeInstance
	sessions map[string]store.ExecutionSession
	next     int
}

func (s *launcherSessionStore) GetRun(_ context.Context, projectID, runID string) (store.Run, error) {
	if s.run.ProjectID != projectID || s.run.ID != runID {
		return store.Run{}, store.ErrNotFound
	}
	return s.run, nil
}

func (s *launcherSessionStore) GetRuntimeInstance(_ context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	if s.instance.ProjectID != projectID || s.instance.ID != instanceID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	return s.instance, nil
}

func (s *launcherSessionStore) CreateExecutionSession(_ context.Context, session store.ExecutionSession) (store.ExecutionSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	session.ID = "session-" + string(rune('0'+s.next))
	if s.sessions == nil {
		s.sessions = make(map[string]store.ExecutionSession)
	}
	s.sessions[session.ID] = session
	return session, nil
}

func (s *launcherSessionStore) GetExecutionSession(_ context.Context, projectID, sessionID string) (store.ExecutionSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.ProjectID != projectID {
		return store.ExecutionSession{}, store.ErrNotFound
	}
	return session, nil
}

func (s *launcherSessionStore) TransitionExecutionSession(_ context.Context, transition store.ExecutionSessionTransition) (store.ExecutionSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[transition.SessionID]
	if !ok || session.ProjectID != transition.ProjectID {
		return store.ExecutionSession{}, store.ErrNotFound
	}
	allowed := false
	for _, status := range transition.FromStatuses {
		allowed = allowed || session.Status == status
	}
	if !allowed {
		return store.ExecutionSession{}, store.ErrConflict
	}
	now := time.Now()
	session.Status = transition.Status
	session.ExitCode = transition.ExitCode
	if transition.Status == "RUNNING" && session.StartedAt == nil {
		session.StartedAt = &now
	}
	if transition.Status == "COMPLETED" || transition.Status == "FAILED" || transition.Status == "CANCELLED" {
		session.CompletedAt = &now
	}
	s.sessions[session.ID] = session
	return session, nil
}

func (s *launcherSessionStore) UpdateRuntimeInstanceRunnerStatus(_ context.Context, projectID, instanceID, status string) (store.RuntimeInstance, error) {
	if s.instance.ProjectID != projectID || s.instance.ID != instanceID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	s.instance.RunnerStatus = status
	return s.instance, nil
}

type launcherPreparer struct{ runtimeID string }

func (p launcherPreparer) Prepare(context.Context, string, string, executioncontext.SecretRequest) (executioncontext.Prepared, error) {
	return executioncontext.Prepared{}, nil
}

type launcherRunnerManager struct{ client runner.Client }

func (m launcherRunnerManager) Connect(context.Context, string, string) (runner.Client, error) {
	return m.client, nil
}

func (m launcherRunnerManager) Reconcile(_ context.Context, _, _, sessionID string) (runner.ProcessSession, bool, error) {
	if m.client == nil {
		return nil, false, nil
	}
	session, err := m.client.Attach(sessionID)
	if err != nil {
		return nil, false, err
	}
	return session, session != nil, nil
}

type launcherClient struct {
	stdout   string
	stderr   string
	exitCode int
	waitErr  error
	done     chan struct{}
	waitGate *launcherWaitGate
}

func newLauncherClient(stdout, stderr string, exitCode int, waitErr error) *launcherClient {
	return &launcherClient{stdout: stdout, stderr: stderr, exitCode: exitCode, waitErr: waitErr, done: make(chan struct{})}
}

func (c *launcherClient) transport(sessionID string) runner.ProcessSession {
	return &launcherTransportProcess{
		id:       sessionID,
		stdout:   strings.NewReader(c.stdout),
		stderr:   strings.NewReader(c.stderr),
		stdin:    &launcherStdin{},
		result:   runner.Result{ExitCode: c.exitCode},
		waitErr:  c.waitErr,
		waitGate: c.waitGate,
	}
}

func (c *launcherClient) Start(_ context.Context, sessionID string, _ runner.Request) (runner.ProcessSession, error) {
	return c.transport(sessionID), nil
}
func (*launcherClient) Capabilities() protocol.Capabilities { return protocol.Capabilities{} }
func (*launcherClient) Health() protocol.Health             { return protocol.Health{} }
func (c *launcherClient) Attach(sessionID string) (runner.ProcessSession, error) {
	return c.transport(sessionID), nil
}
func (c *launcherClient) Done() <-chan struct{} { return c.done }
func (*launcherClient) Err() error              { return nil }
func (c *launcherClient) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}

type launcherStdin struct{ bytes.Buffer }

func (*launcherStdin) Close() error { return nil }

type launcherTransportProcess struct {
	id       string
	stdout   io.Reader
	stderr   io.Reader
	stdin    io.WriteCloser
	result   runner.Result
	waitErr  error
	waitGate *launcherWaitGate
}

type launcherWaitGate struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newLauncherWaitGate() *launcherWaitGate {
	return &launcherWaitGate{started: make(chan struct{}), release: make(chan struct{})}
}

func (g *launcherWaitGate) stop() {
	g.once.Do(func() { close(g.release) })
}

func (p *launcherTransportProcess) ID() string            { return p.id }
func (p *launcherTransportProcess) Stdout() io.Reader     { return p.stdout }
func (p *launcherTransportProcess) Stderr() io.Reader     { return p.stderr }
func (p *launcherTransportProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *launcherTransportProcess) Wait(ctx context.Context) (runner.Result, error) {
	if p.waitGate != nil {
		close(p.waitGate.started)
		select {
		case <-p.waitGate.release:
		case <-ctx.Done():
			return runner.Result{}, ctx.Err()
		}
	}
	return p.result, p.waitErr
}
func (p *launcherTransportProcess) Terminate(context.Context) error {
	if p.waitGate != nil {
		p.waitGate.stop()
	}
	return nil
}
func (p *launcherTransportProcess) Kill(context.Context) error {
	if p.waitGate != nil {
		p.waitGate.stop()
	}
	return nil
}

type failingLauncherSessions struct{ err error }

func (s failingLauncherSessions) Start(context.Context, string, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	return nil, s.err
}
func (s failingLauncherSessions) Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error) {
	return nil, s.err
}
func (failingLauncherSessions) ReconcileAll(context.Context) error { return nil }

func TestProcessLauncherCapturesAuthorizedProcessEvidence(t *testing.T) {
	cases := []struct {
		name           string
		exitCode       int
		waitErr        error
		wantEvent      string
		wantErr        bool
		requestKind    string
		stopBeforeWait bool
	}{
		{name: "test success", exitCode: 0, wantEvent: "test.completed", requestKind: "test", stopBeforeWait: true},
		{name: "tool nonzero exit", exitCode: 7, wantEvent: "tool.failed", wantErr: false, requestKind: "tool"},
		{name: "tool stopped after terminate", exitCode: 137, wantEvent: "tool.stopped", requestKind: "tool", stopBeforeWait: true},
		{name: "tool wait failure after stop", exitCode: 137, waitErr: errors.New("transport ended"), wantEvent: "tool.failed", wantErr: true, requestKind: "tool", stopBeforeWait: true},
		{name: "test wait failure", exitCode: 1, waitErr: errors.New("transport ended"), wantEvent: "test.failed", wantErr: true, requestKind: "test", stopBeforeWait: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			client := newLauncherClient(strings.Repeat("stdout-", 8), "stderr-data", tc.exitCode, tc.waitErr)
			transportSessions, err := app.NewExecutionSessionService(sessionStore, launcherRunnerManager{client: client}, nil)
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
			process, err := launcher.Start(t.Context(), engine.ProcessRequest{Kind: tc.requestKind, Name: "fixture", Command: []string{"fixture"}, CWD: "/workspace"})
			if err != nil {
				t.Fatal(err)
			}
			if process.ID() == "" {
				t.Fatal("process ID is empty")
			}
			if process.Stdin() == nil {
				t.Fatal("process stdin is nil")
			}
			if tc.stopBeforeWait {
				if err := process.Terminate(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err := process.Kill(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			stdoutDone := make(chan struct{})
			stderrDone := make(chan struct{})
			go func() { _, _ = io.Copy(io.Discard, process.Stdout()); close(stdoutDone) }()
			go func() { _, _ = io.Copy(io.Discard, process.Stderr()); close(stderrDone) }()
			result, waitErr := process.Wait(t.Context())
			<-stdoutDone
			<-stderrDone
			if tc.wantErr != (waitErr != nil) {
				t.Fatalf("Wait() error=%v wantErr=%v", waitErr, tc.wantErr)
			}
			if result.ExitCode != tc.exitCode {
				t.Fatalf("exit code=%d want=%d", result.ExitCode, tc.exitCode)
			}
			if _, secondErr := process.Wait(t.Context()); tc.wantErr != (secondErr != nil) {
				t.Fatalf("second Wait() error=%v wantErr=%v", secondErr, tc.wantErr)
			}
			if !hasProcessTestEvent(evidenceStore.events, tc.wantEvent) {
				t.Fatalf("missing event %q in %+v", tc.wantEvent, evidenceStore.events)
			}
			if tc.wantEvent == "tool.failed" {
				failed := processTestEvent(evidenceStore.events, "tool.failed")
				var payload map[string]any
				if err := json.Unmarshal(failed.Payload, &payload); err != nil {
					t.Fatalf("decode tool.failed payload: %v", err)
				}
				if payload["name"] != "fixture" || payload["reason"] == nil || payload["reason"] == "" {
					t.Fatalf("flattened tool.failed payload=%+v", payload)
				}
				if _, nested := payload["result"]; nested {
					t.Fatalf("tool.failed nested result unexpectedly present: %+v", payload)
				}
			}
			if tc.wantEvent == "tool.stopped" {
				stopped := processTestEvent(evidenceStore.events, "tool.stopped")
				var payload map[string]any
				if err := json.Unmarshal(stopped.Payload, &payload); err != nil {
					t.Fatalf("decode tool.stopped payload: %v", err)
				}
				if payload["name"] != "fixture" {
					t.Fatalf("tool.stopped name=%v want fixture in %+v", payload["name"], payload)
				}
				if payload["reason"] != nil && payload["reason"] != "" {
					t.Fatalf("tool.stopped reason unexpectedly present: %+v", payload)
				}
				code, _ := payload["exitCode"].(float64)
				if int(code) != tc.exitCode {
					t.Fatalf("tool.stopped exitCode=%v want %d in %+v", payload["exitCode"], tc.exitCode, payload)
				}
				if hasProcessTestEvent(evidenceStore.events, "tool.failed") {
					t.Fatalf("intentional stop recorded tool.failed: %+v", evidenceStore.events)
				}
			}
			if len(evidenceStore.chunks) < 2 {
				t.Fatalf("raw output chunks=%d, want at least stdout and stderr evidence", len(evidenceStore.chunks))
			}
		})
	}
}

func TestProcessLauncherRecordsStoppedWhenTerminatedDuringWait(t *testing.T) {
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
	gate := newLauncherWaitGate()
	client := newLauncherClient("", "", 137, nil)
	client.waitGate = gate
	transportSessions, err := app.NewExecutionSessionService(sessionStore, launcherRunnerManager{client: client}, nil)
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
	process, err := launcher.Start(t.Context(), engine.ProcessRequest{
		Kind:    "tool",
		Name:    "opencode-server",
		Command: []string{"opencode", "serve"},
		CWD:     "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() {
		_, waitErr := process.Wait(t.Context())
		waitDone <- waitErr
	}()
	select {
	case <-gate.started:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not start")
	}
	if err := process.Terminate(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			t.Fatalf("Wait() error=%v", waitErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return after Terminate()")
	}
	if !hasProcessTestEvent(evidenceStore.events, "tool.stopped") {
		t.Fatalf("missing tool.stopped in %+v", evidenceStore.events)
	}
	if hasProcessTestEvent(evidenceStore.events, "tool.failed") {
		t.Fatalf("intentional stop recorded tool.failed: %+v", evidenceStore.events)
	}
}

func TestProcessLauncherRecordsStartFailure(t *testing.T) {
	safe := processTestSafeContext(t.TempDir())
	evidenceStore := &processTestStore{}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(evidenceStore, blobs, 32)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("launch unavailable")
	launcher := &processLauncher{
		sessions:          failingLauncherSessions{err: want},
		events:            recorder,
		output:            output,
		safe:              safe,
		runtimeInstanceID: "runtime-instance",
		scope:             evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
	}
	if _, err := launcher.Start(t.Context(), engine.ProcessRequest{Kind: "tool", Name: "fixture", Command: []string{"fixture"}}); !errors.Is(err, want) {
		t.Fatalf("Start() error=%v want=%v", err, want)
	}
	if !hasProcessTestEvent(evidenceStore.events, "tool.started") || !hasProcessTestEvent(evidenceStore.events, "tool.failed") {
		t.Fatalf("start failure events=%+v", evidenceStore.events)
	}
	failed := processTestEvent(evidenceStore.events, "tool.failed")
	var payload map[string]any
	if err := json.Unmarshal(failed.Payload, &payload); err != nil {
		t.Fatalf("decode tool.failed payload: %v", err)
	}
	if payload["name"] != "fixture" {
		t.Fatalf("tool.failed name=%v want fixture in %+v", payload["name"], payload)
	}
	command, _ := payload["command"].([]any)
	if len(command) != 1 || command[0] != "fixture" {
		t.Fatalf("tool.failed command=%v want [fixture] in %+v", payload["command"], payload)
	}
	if payload["reason"] != "launch unavailable" {
		t.Fatalf("tool.failed reason=%v want launch unavailable in %+v", payload["reason"], payload)
	}
	if _, nested := payload["result"]; nested {
		t.Fatalf("tool.failed nested result unexpectedly present: %+v", payload)
	}
}

func TestProcessLauncherAttachDoesNotRecordToolStarted(t *testing.T) {
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
		run: store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, WorkspaceID: safe.Workspace.ID},
		instance: store.RuntimeInstance{
			ID: "runtime-instance", ProjectID: safe.Project.ID, WorkspaceID: safe.Workspace.ID,
			Status: "RUNNING",
		},
		sessions: map[string]store.ExecutionSession{
			"session-1": {
				ID: "session-1", ProjectID: safe.Project.ID, RunID: safe.Run.ID,
				RuntimeInstanceID: "runtime-instance", Status: "RUNNING",
			},
		},
	}
	client := newLauncherClient("stdout", "stderr", 0, nil)
	transportSessions, err := app.NewExecutionSessionService(sessionStore, launcherRunnerManager{client: client}, nil)
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
		attachSessionID:   "session-1",
		scope:             evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
	}
	process, err := launcher.Attach(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if process.ID() != "session-1" {
		t.Fatalf("attached id=%q", process.ID())
	}
	go func() { _, _ = io.Copy(io.Discard, process.Stdout()) }()
	go func() { _, _ = io.Copy(io.Discard, process.Stderr()) }()
	if _, err := process.Wait(t.Context()); err != nil {
		t.Fatalf("Wait() error=%v", err)
	}
	if hasProcessTestEvent(evidenceStore.events, "tool.started") {
		t.Fatalf("attach must not record tool.started: %+v", evidenceStore.events)
	}
	var completed *store.Event
	for index := range evidenceStore.events {
		if evidenceStore.events[index].Type == "tool.completed" {
			completed = &evidenceStore.events[index]
			break
		}
	}
	if completed == nil {
		t.Fatalf("missing tool.completed in %+v", evidenceStore.events)
	}
	if completed.ParentEventID != nil {
		t.Fatalf("attached tool.completed parent=%v", *completed.ParentEventID)
	}
}

func TestProcessLauncherAttachFailureBoundaries(t *testing.T) {
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

	t.Run("missing session", func(t *testing.T) {
		launcher := &processLauncher{
			sessions:          failingLauncherSessions{},
			events:            recorder,
			output:            output,
			safe:              safe,
			runtimeInstanceID: "runtime-instance",
			scope:             evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
		}
		if _, err := launcher.Attach(t.Context()); !errors.Is(err, engine.ErrNotAttachable) {
			t.Fatalf("Attach() error=%v", err)
		}
	})

	t.Run("session error", func(t *testing.T) {
		want := errors.New("session attach failed")
		launcher := &processLauncher{
			sessions:          failingLauncherSessions{err: want},
			events:            recorder,
			output:            output,
			safe:              safe,
			runtimeInstanceID: "runtime-instance",
			attachSessionID:   "session-1",
			scope:             evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
		}
		if _, err := launcher.Attach(t.Context()); !errors.Is(err, want) {
			t.Fatalf("Attach() error=%v want=%v", err, want)
		}
	})

	t.Run("unavailable process", func(t *testing.T) {
		launcher := &processLauncher{
			sessions:          failingLauncherSessions{},
			events:            recorder,
			output:            output,
			safe:              safe,
			runtimeInstanceID: "runtime-instance",
			attachSessionID:   "session-1",
			scope:             evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
		}
		if _, err := launcher.Attach(t.Context()); err == nil || !strings.Contains(err.Error(), "attached Execution Session is unavailable") {
			t.Fatalf("Attach() error=%v", err)
		}
	})
}
