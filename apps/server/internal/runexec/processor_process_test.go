package runexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type processTestStore struct {
	provenance      json.RawMessage
	events          []store.Event
	artifacts       []store.Artifact
	chunks          []store.RawOutputChunk
	sessions        []store.ExecutionSession
	listSessionsErr error
}

func (s *processTestStore) PutRunProvenance(_ context.Context, _, _ string, value json.RawMessage) error {
	if len(s.provenance) != 0 {
		return store.ErrConflict
	}
	s.provenance = append(json.RawMessage(nil), value...)
	return nil
}

func (s *processTestStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) {
	if len(s.provenance) == 0 {
		return nil, store.ErrNotFound
	}
	return append(json.RawMessage(nil), s.provenance...), nil
}

func (s *processTestStore) ListExecutionSessions(context.Context, string, []string) ([]store.ExecutionSession, error) {
	if s.listSessionsErr != nil {
		return nil, s.listSessionsErr
	}
	return append([]store.ExecutionSession(nil), s.sessions...), nil
}

func (s *processTestStore) AppendEvent(_ context.Context, event store.Event) (store.Event, error) {
	sequence := int64(len(s.events) + 1)
	event.ID = "event-" + strings.TrimSpace(event.Type)
	event.Sequence = &sequence
	s.events = append(s.events, event)
	return event, nil
}

func (s *processTestStore) CreateRawOutputChunk(_ context.Context, chunk store.RawOutputChunk) (store.RawOutputChunk, error) {
	chunk.ID = "chunk"
	s.chunks = append(s.chunks, chunk)
	return chunk, nil
}

func (s *processTestStore) ListRawOutputChunks(context.Context, string, string) ([]store.RawOutputChunk, error) {
	return append([]store.RawOutputChunk(nil), s.chunks...), nil
}

func (s *processTestStore) CreateArtifact(_ context.Context, artifact store.Artifact) (store.Artifact, error) {
	artifact.ID = "artifact-" + artifact.Kind + "-" + strings.ReplaceAll(artifact.Name, "/", "-")
	s.artifacts = append(s.artifacts, artifact)
	return artifact, nil
}

func (s *processTestStore) GetWorkspace(_ context.Context, projectID, workspaceID string) (store.Workspace, error) {
	return store.Workspace{ID: workspaceID, ProjectID: projectID, WorkingBranch: "agent-board/AB-1", BootstrapStatus: "READY"}, nil
}

func (s *processTestStore) UpdateWorkspaceCurrentBranch(_ context.Context, projectID, workspaceID, currentBranch string) (store.Workspace, error) {
	branch := currentBranch
	return store.Workspace{ID: workspaceID, ProjectID: projectID, WorkingBranch: "agent-board/AB-1", CurrentBranch: &branch, BootstrapStatus: "READY"}, nil
}

type processTestResolver struct {
	resolved executioncontext.Resolved
	err      error
}

func (r processTestResolver) Resolve(context.Context, string, string) (executioncontext.Resolved, error) {
	return r.resolved, r.err
}

type processTestSessions struct{}

func (processTestSessions) Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error) {
	return nil, errors.New("unexpected process attach")
}

func (processTestSessions) ReconcileAll(context.Context) error { return nil }

type processTestEngine struct {
	workspace string
	fail      error
}

func (e processTestEngine) Name() string { return "test" }

func (e processTestEngine) Execute(_ context.Context, request engine.Request) (engine.Result, error) {
	if e.fail != nil {
		return engine.Result{}, e.fail
	}
	// Runner-owned execution must never mutate the authoritative server checkout.
	if request.Context.Runner != nil {
		return engine.Result{Summary: "test engine completed"}, nil
	}
	if strings.TrimSpace(e.workspace) == "" {
		return engine.Result{Summary: "test engine completed"}, nil
	}
	if err := os.WriteFile(filepath.Join(e.workspace, "new.txt"), []byte("finalized-value\n"), 0o644); err != nil {
		return engine.Result{}, err
	}
	file, err := os.OpenFile(filepath.Join(e.workspace, "tracked.txt"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return engine.Result{}, err
	}
	_, writeErr := file.WriteString("changed\n")
	closeErr := file.Close()
	return engine.Result{Summary: "test engine completed"}, errors.Join(writeErr, closeErr)
}

type processTestLifecycle struct{ run store.Run }

func (l processTestLifecycle) Running(context.Context) (store.Run, error) { return l.run, nil }

func TestProcessorProcessFailsWhenMultipleLiveRunnerSessionsExist(t *testing.T) {
	repository := initProcessTestRepository(t)
	safe := processTestSafeContext(repository)
	evidenceStore := &processTestStore{sessions: []store.ExecutionSession{
		{ID: "session-a", ProjectID: safe.Project.ID, RunID: safe.Run.ID, RunnerID: "runner-1", Status: "RUNNING"},
		{ID: "session-b", ProjectID: safe.Project.ID, RunID: safe.Run.ID, RunnerID: "runner-1", Status: "STARTING"},
	}}
	processor := buildProcessTestProcessor(t, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, evidenceStore)
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "RUNNING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
	if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "multiple live Execution Sessions") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProcessorProcessReturnsFailedResultForResolverAndSessionListErrors(t *testing.T) {
	repository := initProcessTestRepository(t)
	safe := processTestSafeContext(repository)
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

	t.Run("resolver", func(t *testing.T) {
		processor := buildProcessTestProcessor(t, processTestResolver{err: errors.New("resolver unavailable")}, &processTestStore{})
		result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "resolver unavailable") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("list sessions", func(t *testing.T) {
		evidenceStore := &processTestStore{listSessionsErr: errors.New("session list failed")}
		processor := buildProcessTestProcessor(t, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, evidenceStore)
		result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "session list failed") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
}

func buildProcessTestProcessor(t *testing.T, resolver ContextResolver, evidenceStore *processTestStore) *Processor {
	t.Helper()
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(evidenceStore, blobs, 64)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := engine.NewRegistry(processTestEngine{})
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewProcessor(evidenceStore, resolver, processTestSessions{}, registry, recorder, output, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func TestProcessorHelpersCoverFailureAndPayloadShapes(t *testing.T) {
	if _, err := NewProcessor(nil, nil, nil, nil, nil, nil, nil, nil); err == nil {
		t.Fatal("expected dependency validation error")
	}
	if got := safeFailure(nil); got != "execution failed" {
		t.Fatalf("safeFailure(nil)=%q", got)
	}
	long := strings.Repeat("x", 1200)
	if got := safeFailure(errors.New(long)); len(got) != 1024 {
		t.Fatalf("safeFailure length=%d", len(got))
	}
	exitZero := 0
	testPayload, ok := processPayload(engine.ProcessRequest{Kind: "test", Command: []string{"go", "test"}}, &exitZero, []string{"chunk"}).(evidence.TestPayload)
	if !ok || testPayload.Status != "passed" || len(testPayload.OutputChunkIDs) != 1 {
		t.Fatalf("test payload=%+v", testPayload)
	}
	exitOne := 1
	failedPayload := processPayload(engine.ProcessRequest{Kind: "test"}, &exitOne, nil).(evidence.TestPayload)
	if failedPayload.Status != "failed" {
		t.Fatalf("failed test payload=%+v", failedPayload)
	}
	toolPayload := processPayload(engine.ProcessRequest{Kind: "tool", Name: "x"}, nil, nil).(evidence.ToolPayload)
	if toolPayload.Name != "x" {
		t.Fatalf("tool payload=%+v", toolPayload)
	}
	flattened := flattenFailurePayload(evidence.ToolPayload{Kind: "tool", Name: "opencode-server", Command: []string{"opencode", "serve"}}, errors.New("process exited with code 137"))
	mapped, ok := flattened.(map[string]any)
	if !ok || mapped["name"] != "opencode-server" || mapped["reason"] != "process exited with code 137" {
		t.Fatalf("flattened=%+v", flattened)
	}
	if _, nested := mapped["result"]; nested {
		t.Fatalf("flattened nested result: %+v", mapped)
	}
	if fallback, ok := flattenFailurePayload(make(chan int), errors.New("boom")).(map[string]any); !ok || fallback["reason"] != "boom" {
		t.Fatalf("unencodable payload fallback=%#v", fallback)
	}
	original := map[string]string{"A": "B"}
	cloned := cloneMap(original)
	cloned["A"] = "C"
	if original["A"] != "B" || cloneMap(nil) != nil {
		t.Fatal("cloneMap did not isolate input")
	}
	if ids := chunkIDs([]store.RawOutputChunk{{ID: "a"}, {ID: "b"}}); strings.Join(ids, ",") != "a,b" {
		t.Fatalf("chunk ids=%v", ids)
	}
}

func initProcessTestRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	for _, command := range [][]string{{"git", "init", "-q", "-b", "agent-board/AB-1"}, {"git", "config", "user.email", "test@example.invalid"}, {"git", "config", "user.name", "Agent Board Test"}} {
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = repository
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", command, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"git", "add", "."}, {"git", "commit", "-qm", "baseline"}} {
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = repository
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", command, err, output)
		}
	}
	return repository
}

func processTestSafeContext(repository string) executioncontext.SafeContext {
	var baseRevision *string
	if output, err := exec.Command("git", "-C", repository, "rev-parse", "HEAD").Output(); err == nil {
		revision := strings.TrimSpace(string(output))
		if revision != "" {
			baseRevision = &revision
		}
	}
	return executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project", Name: "Project"},
		Issue:     executioncontext.IssueContext{ID: "issue", Title: "Issue"},
		Run:       executioncontext.RunContext{ID: "run", Attempt: 1},
		Agent:     executioncontext.AgentContext{ID: "agent", Name: "Agent", Engine: "test"},
		Model:     executioncontext.ModelContext{ID: "model", Name: "Model", Model: "model"},
		Provider:  executioncontext.ProviderContext{ID: "provider", Name: "Provider", Kind: "test"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace", Path: repository, BaseRevision: baseRevision, WorkingBranch: "agent-board/AB-1", BootstrapStatus: "READY"},
	}
}

func assertProcessTestRepositoryFinalized(t *testing.T, repository string, safe executioncontext.SafeContext) {
	t.Helper()
	status := exec.Command("git", "-C", repository, "status", "--porcelain")
	if output, err := status.CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("repository not clean after finalization: %q err=%v", output, err)
	}
	if safe.Workspace.BaseRevision == nil || strings.TrimSpace(*safe.Workspace.BaseRevision) == "" {
		t.Fatal("test Workspace baseline is missing")
	}
	head, err := exec.Command("git", "-C", repository, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(head)) == strings.TrimSpace(*safe.Workspace.BaseRevision) {
		t.Fatal("terminal finalization did not advance the Issue branch")
	}
	ancestor := exec.Command("git", "-C", repository, "merge-base", "--is-ancestor", *safe.Workspace.BaseRevision, "HEAD")
	if output, err := ancestor.CombinedOutput(); err != nil {
		t.Fatalf("finalized branch lost execution baseline: %v: %s", err, output)
	}
}

func hasProcessTestEvent(events []store.Event, eventType string) bool {
	return processTestEvent(events, eventType).Type == eventType
}

func processTestEvent(events []store.Event, eventType string) store.Event {
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	return store.Event{}
}

func TestProcessorRecordOmitsEmptyParentEventID(t *testing.T) {
	evidenceStore := &processTestStore{}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{events: recorder}
	empty := ""
	if err := processor.record(context.Background(), processTestSafeContext(t.TempDir()), "agent.message", map[string]any{"message": "ok"}, &empty); err != nil {
		t.Fatal(err)
	}
	if len(evidenceStore.events) != 1 || evidenceStore.events[0].ParentEventID != nil {
		t.Fatalf("empty parent id must be omitted: %+v", evidenceStore.events)
	}
}

func TestProcessTestStoreRawOutputRoundTrip(t *testing.T) {
	store := &processTestStore{}
	chunk, err := store.CreateRawOutputChunk(t.Context(), storepkgRawChunk("run-1", "hello"))
	if err != nil || chunk.ID == "" {
		t.Fatalf("chunk=%+v err=%v", chunk, err)
	}
	listed, err := store.ListRawOutputChunks(t.Context(), "project", "run-1")
	if err != nil || len(listed) != 1 || !bytes.Equal([]byte(listed[0].StorageRef), []byte("hello")) {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
}

func storepkgRawChunk(runID, storageRef string) store.RawOutputChunk {
	return store.RawOutputChunk{RunID: runID, StorageRef: storageRef}
}

var _ scheduler.Lifecycle = processTestLifecycle{}
