package runexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type waitingExecutionStore struct {
	*processTestStore
	*questionTestStore
}

type canceledQuestionLookupStore struct {
	*waitingExecutionStore
}

func (s *canceledQuestionLookupStore) GetOpenBlockingQuestion(ctx context.Context, _, _ string) (store.Question, error) {
	return store.Question{}, ctx.Err()
}

type failingDestroyRuntime struct {
	*processTestRuntime
	err error
}

func (r *failingDestroyRuntime) Destroy(_ context.Context, _, _ string) (store.RuntimeInstance, error) {
	r.destroyed++
	return store.RuntimeInstance{}, r.err
}

func waitingSafeContext() executioncontext.SafeContext {
	return executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project-1"},
		Issue:     executioncontext.IssueContext{ID: "issue-1"},
		Run:       executioncontext.RunContext{ID: "run-1"},
		Agent:     executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
	}
}

func TestFinishWaitingForInputUsesPersistedQuestionAndCleansRuntime(t *testing.T) {
	question := store.Question{ID: "question-1", ProjectID: "project-1", IssueID: "issue-1", RunID: "run-1", Blocking: true, Status: "OPEN"}
	base := &processTestStore{}
	questions := &questionTestStore{open: &question}
	combined := &waitingExecutionStore{processTestStore: base, questionTestStore: questions}
	recorder, err := evidence.NewRecorder(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtimes := &processTestRuntime{}
	processor := &Processor{store: combined, runtimes: runtimes, events: recorder}
	instance := store.RuntimeInstance{ID: "runtime-instance-1", ProjectID: "project-1", Status: "RUNNING"}

	result, err := processor.finishWaitingForInput(context.Background(), waitingSafeContext(), instance)
	if err != nil {
		t.Fatalf("finishWaitingForInput() error=%v", err)
	}
	if result.RunStatus != "WAITING_FOR_INPUT" {
		t.Fatalf("result=%+v", result)
	}
	if runtimes.stopped != 1 || runtimes.destroyed != 1 {
		t.Fatalf("runtime cleanup stop/destroy=%d/%d", runtimes.stopped, runtimes.destroyed)
	}
	seenWaiting := false
	for _, event := range base.events {
		if event.Type == "run.waiting_for_input" {
			seenWaiting = true
		}
	}
	if !seenWaiting {
		t.Fatalf("events=%+v", base.events)
	}
}

func TestFinishWaitingForInputPropagatesCanceledQuestionLookup(t *testing.T) {
	base := &processTestStore{}
	combined := &canceledQuestionLookupStore{waitingExecutionStore: &waitingExecutionStore{processTestStore: base, questionTestStore: &questionTestStore{}}}
	recorder, err := evidence.NewRecorder(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtimes := &processTestRuntime{}
	processor := &Processor{store: combined, runtimes: runtimes, events: recorder}
	instance := store.RuntimeInstance{ID: "runtime-instance-1", ProjectID: "project-1", Status: "RUNNING"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := processor.finishWaitingForInput(ctx, waitingSafeContext(), instance)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("finishWaitingForInput() error=%v, want context.Canceled", err)
	}
	if result.RunStatus != "" || result.FailureReason != nil {
		t.Fatalf("result=%+v", result)
	}
	if runtimes.stopped != 1 || runtimes.destroyed != 1 {
		t.Fatalf("runtime cleanup stop/destroy=%d/%d", runtimes.stopped, runtimes.destroyed)
	}
	for _, event := range base.events {
		if event.Type == "run.failed" || event.Type == "run.waiting_for_input" {
			t.Fatalf("cancellation must not terminally project the Run: %+v", base.events)
		}
	}
}

func TestFinishWaitingForInputFailsWhenRuntimeCleanupFails(t *testing.T) {
	question := store.Question{ID: "question-1", ProjectID: "project-1", IssueID: "issue-1", RunID: "run-1", Blocking: true, Status: "OPEN"}
	base := &processTestStore{}
	combined := &waitingExecutionStore{processTestStore: base, questionTestStore: &questionTestStore{open: &question}}
	recorder, err := evidence.NewRecorder(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtimes := &failingDestroyRuntime{processTestRuntime: &processTestRuntime{}, err: errors.New("destroy failed")}
	processor := &Processor{store: combined, runtimes: runtimes, events: recorder}

	result, err := processor.finishWaitingForInput(context.Background(), waitingSafeContext(), store.RuntimeInstance{ID: "runtime-instance-1", ProjectID: "project-1", Status: "RUNNING"})
	if err != nil {
		t.Fatalf("finishWaitingForInput() top-level error=%v", err)
	}
	if result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "destroy failed") {
		t.Fatalf("result=%+v", result)
	}
	if runtimes.stopped != 1 || runtimes.destroyed != 1 {
		t.Fatalf("runtime cleanup stop/destroy=%d/%d", runtimes.stopped, runtimes.destroyed)
	}
	for _, event := range base.events {
		if event.Type == "run.waiting_for_input" {
			t.Fatalf("cleanup failure must not emit waiting event: %+v", base.events)
		}
	}
}

func TestFinishWaitingForInputFailsWithoutQuestionStoreCapability(t *testing.T) {
	base := &processTestStore{}
	recorder, err := evidence.NewRecorder(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtimes := &processTestRuntime{}
	processor := &Processor{store: base, runtimes: runtimes, events: recorder}

	result, err := processor.finishWaitingForInput(context.Background(), waitingSafeContext(), store.RuntimeInstance{ID: "runtime-instance-1", ProjectID: "project-1", Status: "RUNNING"})
	if err != nil {
		t.Fatalf("finishWaitingForInput() unexpected top-level error=%v", err)
	}
	if result.RunStatus != "FAILED" || result.FailureReason == nil {
		t.Fatalf("result=%+v", result)
	}
	if runtimes.stopped != 1 || runtimes.destroyed != 1 {
		t.Fatalf("runtime cleanup stop/destroy=%d/%d", runtimes.stopped, runtimes.destroyed)
	}
}
