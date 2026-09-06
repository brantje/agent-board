package runexec

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type waitingExecutionStore struct {
	*processTestStore
	*questionTestStore
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
	safe := executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project-1"},
		Issue:     executioncontext.IssueContext{ID: "issue-1"},
		Run:       executioncontext.RunContext{ID: "run-1"},
		Agent:     executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
	}
	instance := store.RuntimeInstance{ID: "runtime-instance-1", ProjectID: "project-1", Status: "RUNNING"}

	result, err := processor.finishWaitingForInput(context.Background(), safe, instance)
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

func TestFinishWaitingForInputFailsWithoutQuestionStoreCapability(t *testing.T) {
	base := &processTestStore{}
	recorder, err := evidence.NewRecorder(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtimes := &processTestRuntime{}
	processor := &Processor{store: base, runtimes: runtimes, events: recorder}
	safe := executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project-1"},
		Issue:     executioncontext.IssueContext{ID: "issue-1"},
		Run:       executioncontext.RunContext{ID: "run-1"},
		Agent:     executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
	}

	result, err := processor.finishWaitingForInput(context.Background(), safe, store.RuntimeInstance{ID: "runtime-instance-1", ProjectID: "project-1", Status: "RUNNING"})
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
