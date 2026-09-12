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
	sessions map[string]store.ExecutionSession
	next     int
}

func (s *launcherSessionStore) GetRun(_ context.Context, projectID, runID string) (store.Run, error) {
	if s.run.ProjectID != projectID || s.run.ID != runID {
		return store.Run{}, store.ErrNotFound
	}
	return s.run, nil
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

func (s *launcherSessionStore) ListExecutionSessionsByRunner(_ context.Context, runnerID string, statuses []string) ([]store.ExecutionSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.ExecutionSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		if session.RunnerID != runnerID {
			continue
		}
		if len(statuses) > 0 {
			matched := false
			for _, status := range statuses {
				if session.Status == status {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, session)
	}
	return out, nil
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
		if session.Status == status {
			allowed = true
			break
		}
	}
	if !allowed {
		return store.ExecutionSession{}, store.ErrConflict
	}
	now := time.Now()
	session.Status = transition.Status
	session.ExitCode = transition.ExitCode
	if len(transition.CommandArgv) > 0 {
		session.CommandArgv = append(json.RawMessage(nil), transition.CommandArgv...)
	}
	if transition.Status == "RUNNING" && session.StartedAt == nil {
		session.StartedAt = &now
	}
	if transition.Status == "COMPLETED" || transition.Status == "FAILED" || transition.Status == "CANCELLED" {
		session.CompletedAt = &now
	}
	s.sessions[session.ID] = session
	return session, nil
}

type launcherPreparer struct{}

func (launcherPreparer) Prepare(context.Context, string, string, executioncontext.SecretRequest) (executioncontext.Prepared, error) {
	return executioncontext.Prepared{}, nil
}

type launcherRunnerRegistry struct{ client runner.Client }

func (m launcherRunnerRegistry) Connect(context.Context, string, string) (runner.Client, error) {
	return m.client, nil
}

func (m launcherRunnerRegistry) Reconcile(_ context.Context, _, _, sessionID string) (runner.ProcessSession, bool, error) {
	if m.client == nil {
		return nil, false, nil
	}
	session, err := m.client.Attach(sessionID)
	if err != nil {
		return nil, false, err
	}
	return session, session != nil, nil
}

func newLauncherAuthorizedSessions(sessionStore app.ExecutionSessionStore, client runner.Client) (*app.AuthorizedExecutionSessionService, error) {
	transport, err := app.NewExecutionSessionService(sessionStore, launcherRunnerRegistry{client: client})
	if err != nil {
		return nil, err
	}
	return app.NewAuthorizedExecutionSessionService(transport, launcherPreparer{})
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

func (g *launcherWaitGate) stop() { g.once.Do(func() { close(g.release) }) }

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

func (s failingLauncherSessions) StartOnRunner(context.Context, string, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	return nil, s.err
}
func (s failingLauncherSessions) StartPreparedOnRunner(context.Context, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	return nil, s.err
}
func (s failingLauncherSessions) Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error) {
	return nil, s.err
}
func (failingLauncherSessions) ReconcileAll(context.Context) error { return nil }

func newLauncher(t *testing.T, safe executioncontext.SafeContext, evidenceStore *processTestStore, client runner.Client) *processLauncher {
	t.Helper()
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
	sessionStore := &launcherSessionStore{run: store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, WorkspaceID: safe.Workspace.ID}}
	sessions, err := newLauncherAuthorizedSessions(sessionStore, client)
	if err != nil {
		t.Fatal(err)
	}
	return &processLauncher{
		sessions: sessions,
		events:   recorder,
		output:   output,
		safe:     safe,
		runnerID: "runner-1",
		scope:    evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
	}
}

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
		{name: "tool nonzero exit", exitCode: 7, wantEvent: "tool.failed", requestKind: "tool"},
		{name: "tool stopped after terminate", exitCode: 137, wantEvent: "tool.stopped", requestKind: "tool", stopBeforeWait: true},
		{name: "tool wait failure after stop", exitCode: 137, waitErr: errors.New("transport ended"), wantEvent: "tool.failed", wantErr: true, requestKind: "tool", stopBeforeWait: true},
		{name: "test wait failure", exitCode: 1, waitErr: errors.New("transport ended"), wantEvent: "test.failed", wantErr: true, requestKind: "test", stopBeforeWait: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			safe := processTestSafeContext(t.TempDir())
			evidenceStore := &processTestStore{}
			client := newLauncherClient(strings.Repeat("stdout-", 8), "stderr-data", tc.exitCode, tc.waitErr)
			launcher := newLauncher(t, safe, evidenceStore, client)
			process, err := launcher.Start(t.Context(), engine.ProcessRequest{Kind: tc.requestKind, Name: "fixture", Command: []string{"fixture"}, CWD: "/workspace"})
			if err != nil {
				t.Fatal(err)
			}
			if process.ID() == "" || process.Stdin() == nil {
				t.Fatal("process transport is incomplete")
			}
			if tc.stopBeforeWait {
				if err := process.Terminate(t.Context()); err != nil {
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
			if !hasProcessTestEvent(evidenceStore.events, tc.wantEvent) {
				t.Fatalf("missing event %q in %+v", tc.wantEvent, evidenceStore.events)
			}
			if len(evidenceStore.chunks) < 2 {
				t.Fatalf("raw output chunks=%d, want stdout and stderr evidence", len(evidenceStore.chunks))
			}
		})
	}
}

func TestProcessLauncherRecordsStoppedWhenTerminatedDuringWait(t *testing.T) {
	safe := processTestSafeContext(t.TempDir())
	evidenceStore := &processTestStore{}
	gate := newLauncherWaitGate()
	client := newLauncherClient("", "", 137, nil)
	client.waitGate = gate
	launcher := newLauncher(t, safe, evidenceStore, client)
	process, err := launcher.Start(t.Context(), engine.ProcessRequest{Kind: "tool", Name: "opencode-server", Command: []string{"opencode", "serve"}, CWD: "/workspace"})
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
	if !hasProcessTestEvent(evidenceStore.events, "tool.stopped") || hasProcessTestEvent(evidenceStore.events, "tool.failed") {
		t.Fatalf("unexpected stop evidence: %+v", evidenceStore.events)
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
		sessions: failingLauncherSessions{err: want},
		events:   recorder,
		output:   output,
		safe:     safe,
		runnerID: "runner-1",
		scope:    evidence.RunScope{ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID},
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
	if payload["name"] != "fixture" || payload["reason"] != "launch unavailable" {
		t.Fatalf("tool.failed payload=%+v", payload)
	}
}
