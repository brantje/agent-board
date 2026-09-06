package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type processTestStore struct {
	provenance json.RawMessage
	events     []store.Event
	artifacts  []store.Artifact
	chunks     []store.RawOutputChunk
	sessions   []store.ExecutionSession
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

type processTestResolver struct {
	resolved executioncontext.Resolved
	err      error
}

func (r processTestResolver) Resolve(context.Context, string, string) (executioncontext.Resolved, error) {
	return r.resolved, r.err
}

type processTestRuntime struct {
	startErr  error
	created   int
	started   int
	stopped   int
	destroyed int
}

func (r *processTestRuntime) Create(_ context.Context, projectID, _, runtimeID string) (store.RuntimeInstance, error) {
	r.created++
	return store.RuntimeInstance{ID: "runtime-instance", ProjectID: projectID, RuntimeID: runtimeID, Status: string(runtimepkg.StateProvisioning)}, nil
}
func (r *processTestRuntime) Start(_ context.Context, projectID, id string) (store.RuntimeInstance, error) {
	r.started++
	if r.startErr != nil {
		return store.RuntimeInstance{ID: id, ProjectID: projectID, Status: string(runtimepkg.StateStarting)}, r.startErr
	}
	return store.RuntimeInstance{ID: id, ProjectID: projectID, RuntimeID: "runtime", Status: string(runtimepkg.StateRunning)}, nil
}
func (r *processTestRuntime) Stop(_ context.Context, projectID, id string, _ runtimepkg.StopReason) (store.RuntimeInstance, error) {
	r.stopped++
	return store.RuntimeInstance{ID: id, ProjectID: projectID, Status: string(runtimepkg.StateStopped)}, nil
}
func (r *processTestRuntime) Destroy(_ context.Context, projectID, id string) (store.RuntimeInstance, error) {
	r.destroyed++
	return store.RuntimeInstance{ID: id, ProjectID: projectID, Status: string(runtimepkg.StateDestroyed)}, nil
}

type processTestSessions struct{}

func (processTestSessions) Start(context.Context, string, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	return nil, errors.New("unexpected process launch")
}
func (processTestSessions) ReconcileAll(context.Context) error { return nil }

type processTestEngine struct {
	workspace string
	fail      error
}

func (e processTestEngine) Name() string { return "test" }
func (e processTestEngine) Execute(context.Context, engine.Request) (engine.Result, error) {
	if e.fail != nil {
		return engine.Result{}, e.fail
	}
	if err := os.WriteFile(filepath.Join(e.workspace, "new.txt"), []byte("snapshot-value\n"), 0o644); err != nil {
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

func TestProcessorProcessPersistsEvidenceAndCleansRuntime(t *testing.T) {
	workspace := initProcessTestRepository(t)
	safe := processTestSafeContext(workspace)
	evidenceStore := &processTestStore{}
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
	candidate, err := evidence.NewCandidateSnapshotter(evidence.NewCandidateCollector(), evidenceStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := engine.NewRegistry(processTestEngine{workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	runtimes := &processTestRuntime{}
	processor, err := NewProcessor(evidenceStore, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, runtimes, processTestSessions{}, registry, recorder, output, candidate)
	if err != nil {
		t.Fatal(err)
	}
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "READY_FOR_REVIEW" {
		t.Fatalf("result=%+v", result)
	}
	if len(evidenceStore.provenance) == 0 {
		t.Fatal("immutable provenance was not persisted")
	}
	if runtimes.created != 1 || runtimes.started != 1 || runtimes.stopped != 1 || runtimes.destroyed != 1 {
		t.Fatalf("runtime lifecycle create/start/stop/destroy=%d/%d/%d/%d", runtimes.created, runtimes.started, runtimes.stopped, runtimes.destroyed)
	}
	for _, eventType := range []string{"run.started", "runtime.provisioning", "runtime.started", "artifact.created", "file.created", "file.modified", "agent.message", "runtime.stopped", "runtime.destroyed", "run.ready_for_review"} {
		if !hasProcessTestEvent(evidenceStore.events, eventType) {
			t.Fatalf("missing event %q in %+v", eventType, evidenceStore.events)
		}
	}
	var fileArtifact store.Artifact
	for _, artifact := range evidenceStore.artifacts {
		if artifact.Kind == "candidate_file" && artifact.Name == "new.txt" {
			fileArtifact = artifact
			break
		}
	}
	if fileArtifact.ID == "" {
		t.Fatalf("candidate_file artifact missing: %+v", evidenceStore.artifacts)
	}
	if err := os.WriteFile(filepath.Join(workspace, "new.txt"), []byte("later-mutation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader, err := blobs.Open(t.Context(), fileArtifact.StorageRef)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "snapshot-value\n" {
		t.Fatalf("immutable artifact=%q", data)
	}
}

func TestProcessorProcessReturnsFailedResultForResolverAndRuntimeErrors(t *testing.T) {
	workspace := initProcessTestRepository(t)
	safe := processTestSafeContext(workspace)
	makeProcessor := func(t *testing.T, resolver processTestResolver, runtimes *processTestRuntime) *Processor {
		t.Helper()
		evidenceStore := &processTestStore{}
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
		candidate, err := evidence.NewCandidateSnapshotter(evidence.NewCandidateCollector(), evidenceStore, blobs)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := engine.NewRegistry(processTestEngine{workspace: workspace})
		if err != nil {
			t.Fatal(err)
		}
		processor, err := NewProcessor(evidenceStore, resolver, runtimes, processTestSessions{}, registry, recorder, output, candidate)
		if err != nil {
			t.Fatal(err)
		}
		return processor
	}
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

	t.Run("resolver", func(t *testing.T) {
		processor := makeProcessor(t, processTestResolver{err: errors.New("resolver unavailable")}, &processTestRuntime{})
		result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "resolver unavailable") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("runtime start", func(t *testing.T) {
		runtimes := &processTestRuntime{startErr: errors.New("runtime start failed")}
		processor := makeProcessor(t, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, runtimes)
		result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
		if err != nil || result.RunStatus != "FAILED" || runtimes.destroyed != 1 {
			t.Fatalf("result=%+v destroyed=%d err=%v", result, runtimes.destroyed, err)
		}
	})
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
	original := map[string]string{"A": "B"}
	cloned := cloneMap(original)
	cloned["A"] = "C"
	if original["A"] != "B" || cloneMap(nil) != nil {
		t.Fatal("cloneMap did not isolate input")
	}
	chunks := []store.RawOutputChunk{{ID: "a"}, {ID: "b"}}
	ids := chunkIDs(chunks)
	if strings.Join(ids, ",") != "a,b" {
		t.Fatalf("chunk ids=%v", ids)
	}
}

func initProcessTestRepository(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	for _, command := range [][]string{{"git", "init", "-q"}, {"git", "config", "user.email", "test@example.invalid"}, {"git", "config", "user.name", "Agent Board Test"}} {
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = workspace
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", command, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = workspace
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	cmd = exec.Command("git", "commit", "-qm", "baseline")
	cmd.Dir = workspace
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	return workspace
}

func processTestSafeContext(workspace string) executioncontext.SafeContext {
	return executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project", Name: "Project"},
		Issue:     executioncontext.IssueContext{ID: "issue", Title: "Issue"},
		Run:       executioncontext.RunContext{ID: "run", Attempt: 1},
		Agent:     executioncontext.AgentContext{ID: "agent", Name: "Agent"},
		Executor:  executioncontext.ExecutorContext{ID: "executor", Name: "Executor", Engine: "test"},
		Model:     executioncontext.ModelContext{ID: "model", Name: "Model", Model: "model"},
		Provider:  executioncontext.ProviderContext{ID: "provider", Name: "Provider", Kind: "test"},
		Runtime:   executioncontext.RuntimeContext{ID: "runtime", Name: "Runtime", Kind: "docker", Image: "image"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace", Path: workspace, WorkingBranch: "work"},
	}
}

func hasProcessTestEvent(events []store.Event, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

var _ scheduler.Lifecycle = processTestLifecycle{}
