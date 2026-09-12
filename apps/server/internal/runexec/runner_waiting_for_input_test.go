package runexec

import (
	"context"
	"errors"
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

func waitingSafeContext() executioncontext.SafeContext {
	return executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project-1"},
		Issue:     executioncontext.IssueContext{ID: "issue-1"},
		Run:       executioncontext.RunContext{ID: "run-1"},
		Agent:     executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
	}
}

func TestFinishWaitingForInputRunnerUsesPersistedBlockingQuestion(t *testing.T) {
	question := store.Question{ID: "question-1", ProjectID: "project-1", IssueID: "issue-1", RunID: "run-1", Blocking: true, Status: "OPEN"}
	base := &processTestStore{}
	combined := &waitingExecutionStore{processTestStore: base, questionTestStore: &questionTestStore{open: &question}}
	recorder, err := evidence.NewRecorder(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{store: combined, events: recorder}

	result, err := processor.finishWaitingForInputRunner(t.Context(), waitingSafeContext())
	if err != nil || result.RunStatus != "WAITING_FOR_INPUT" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(base.events) != 1 || base.events[0].Type != "run.waiting_for_input" {
		t.Fatalf("events=%+v", base.events)
	}
}

func TestFinishWaitingForInputRunnerFailsClosedWithoutPersistedQuestion(t *testing.T) {
	base := &processTestStore{}
	recorder, err := evidence.NewRecorder(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{store: base, events: recorder}
	result, err := processor.finishWaitingForInputRunner(t.Context(), waitingSafeContext())
	if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	combined := &waitingExecutionStore{processTestStore: base, questionTestStore: &questionTestStore{}}
	processor.store = combined
	result, err = processor.finishWaitingForInputRunner(t.Context(), waitingSafeContext())
	if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil {
		t.Fatalf("missing question result=%+v err=%v", result, err)
	}
}

func TestFinishWaitingForInputRunnerPreservesContextCancellation(t *testing.T) {
	base := &processTestStore{}
	combined := &canceledQuestionLookupStore{waitingExecutionStore: &waitingExecutionStore{processTestStore: base, questionTestStore: &questionTestStore{}}}
	recorder, err := evidence.NewRecorder(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{store: combined, events: recorder}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := processor.finishWaitingForInputRunner(ctx, waitingSafeContext())
	if !errors.Is(err, context.Canceled) || result.RunStatus != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
