package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type interactiveQuestionReadStore struct {
	question store.Question
	decision store.Decision
}

func (*interactiveQuestionReadStore) CreateQuestion(context.Context, store.Question) (store.Question, error) {
	return store.Question{}, nil
}
func (s *interactiveQuestionReadStore) GetQuestion(context.Context, string, string) (store.Question, error) {
	if s.question.ID == "" {
		return store.Question{}, store.ErrNotFound
	}
	return s.question, nil
}
func (*interactiveQuestionReadStore) ListQuestions(context.Context, string, store.QuestionFilter) ([]store.Question, error) {
	return nil, nil
}
func (s *interactiveQuestionReadStore) GetDecisionByQuestion(context.Context, string, string) (store.Decision, error) {
	if s.decision.ID == "" {
		return store.Decision{}, store.ErrNotFound
	}
	return s.decision, nil
}
func (*interactiveQuestionReadStore) GetOpenBlockingQuestion(context.Context, string, string) (store.Question, error) {
	return store.Question{}, store.ErrNotFound
}
func (*interactiveQuestionReadStore) AnswerQuestion(context.Context, store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	return store.AnswerQuestionResult{}, nil
}

type interactiveLifecycleStore struct {
	opened        []store.OpenInteractiveQuestionCommand
	openResult    store.OpenInteractiveQuestionResult
	openErr       error
	resolved      []string
	resolveResult store.ResolveInteractiveQuestionResult
	resolveErr    error
}

func (s *interactiveLifecycleStore) OpenInteractiveQuestion(_ context.Context, command store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionResult, error) {
	s.opened = append(s.opened, command)
	return s.openResult, s.openErr
}
func (s *interactiveLifecycleStore) ResolveInteractiveQuestion(_ context.Context, _ string, questionID string) (store.ResolveInteractiveQuestionResult, error) {
	s.resolved = append(s.resolved, questionID)
	return s.resolveResult, s.resolveErr
}

func TestInteractiveQuestionerOpenPersistsCanonicalLifecycleEvidence(t *testing.T) {
	eventStore := &questionEventStore{}
	recorder, err := evidence.NewRecorder(eventStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &interactiveLifecycleStore{openResult: store.OpenInteractiveQuestionResult{
		Question:       store.Question{ID: "question-1", Prompt: "Choose implementation", Kind: "SINGLE_CHOICE", Blocking: true, Status: "OPEN"},
		Created:        true,
		EnteredWaiting: true,
	}}
	q := &interactiveQuestioner{
		store:       &interactiveQuestionReadStore{},
		interactive: lifecycle,
		events:      recorder,
		safe:        interactiveSafeContext(),
		engine:      "opencode",
	}

	opened, err := q.Open(context.Background(), "ses_1/que_1/0", engine.QuestionRequest{
		Prompt:   "Choose implementation",
		Kind:     "SINGLE_CHOICE",
		Blocking: true,
		Options: []engine.QuestionOption{
			{ID: "option-0", Label: "A"},
			{ID: "option-1", Label: "B"},
		},
	})
	if err != nil {
		t.Fatalf("Open() error=%v", err)
	}
	if opened.ID != "question-1" || !opened.Blocking {
		t.Fatalf("opened=%+v", opened)
	}
	if len(lifecycle.opened) != 1 {
		t.Fatalf("commands=%+v", lifecycle.opened)
	}
	command := lifecycle.opened[0]
	if command.Engine != "opencode" || command.CorrelationKey != "ses_1/que_1/0" || command.Question.ProjectID != "project-1" || command.Question.RunID != "run-1" {
		t.Fatalf("command=%+v", command)
	}
	var options []store.QuestionOption
	if err := json.Unmarshal(command.Question.Options, &options); err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 || options[1].ID != "option-1" || options[1].Label != "B" {
		t.Fatalf("options=%+v", options)
	}
	if len(eventStore.events) != 2 || eventStore.events[0].Type != "question.created" || eventStore.events[1].Type != "run.waiting_for_input" {
		t.Fatalf("events=%+v", eventStore.events)
	}
}

func TestInteractiveQuestionerWaitAnswerLoadsDurableDecision(t *testing.T) {
	text := "Use the safer implementation"
	details, err := json.Marshal(map[string]any{"questionAnswer": store.QuestionAnswer{Kind: "TEXT", Text: &text}})
	if err != nil {
		t.Fatal(err)
	}
	questions := &interactiveQuestionReadStore{
		question: store.Question{ID: "question-1", Status: "ANSWERED"},
		decision: store.Decision{ID: "decision-1", SafeDetails: details},
	}
	q := &interactiveQuestioner{store: questions, safe: interactiveSafeContext()}

	answer, err := q.WaitAnswer(context.Background(), "question-1")
	if err != nil {
		t.Fatalf("WaitAnswer() error=%v", err)
	}
	if answer.Kind != "TEXT" || answer.Text == nil || *answer.Text != text {
		t.Fatalf("answer=%+v", answer)
	}
}

func TestInteractiveQuestionerWaitAnswerHandlesTerminalAndCancelledContext(t *testing.T) {
	for _, status := range []string{"CANCELLED", "BROKEN"} {
		q := &interactiveQuestioner{
			store: &interactiveQuestionReadStore{question: store.Question{ID: "question-1", Status: status}},
			safe:  interactiveSafeContext(),
		}
		if _, err := q.WaitAnswer(context.Background(), "question-1"); err == nil {
			t.Fatalf("status %q unexpectedly accepted", status)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q := &interactiveQuestioner{
		store: &interactiveQuestionReadStore{question: store.Question{ID: "question-1", Status: "OPEN"}},
		safe:  interactiveSafeContext(),
	}
	if _, err := q.WaitAnswer(ctx, "question-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitAnswer() error=%v", err)
	}
}

func TestInteractiveQuestionerResolveRecordsResumeOnlyWhenRunResumes(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		eventStore := &questionEventStore{}
		recorder, err := evidence.NewRecorder(eventStore, nil)
		if err != nil {
			t.Fatal(err)
		}
		lifecycle := &interactiveLifecycleStore{resolveResult: store.ResolveInteractiveQuestionResult{Resumed: resumed}}
		q := &interactiveQuestioner{
			interactive: lifecycle,
			events:      recorder,
			safe:        interactiveSafeContext(),
			engine:      "opencode",
		}
		if err := q.Resolve(context.Background(), "question-1"); err != nil {
			t.Fatalf("Resolve() resumed=%v error=%v", resumed, err)
		}
		if len(lifecycle.resolved) != 1 || lifecycle.resolved[0] != "question-1" {
			t.Fatalf("resolved=%v", lifecycle.resolved)
		}
		wantEvents := 1
		if resumed {
			wantEvents = 2
		}
		if len(eventStore.events) != wantEvents {
			t.Fatalf("resumed=%v events=%+v", resumed, eventStore.events)
		}
		if eventStore.events[0].Type != interactiveQuestionResolvedEvent {
			t.Fatalf("resolution event=%+v", eventStore.events[0])
		}
		if resumed && eventStore.events[1].Type != "run.resumed" {
			t.Fatalf("resume event=%+v", eventStore.events[1])
		}
	}
}

func TestInteractiveQuestionerRejectsInvalidCapabilityRequests(t *testing.T) {
	q := &interactiveQuestioner{}
	if _, err := q.Open(context.Background(), "key", engine.QuestionRequest{Prompt: "Question", Kind: "TEXT", Blocking: true}); err == nil {
		t.Fatal("Open() without capabilities unexpectedly succeeded")
	}
	q = &interactiveQuestioner{
		store:       &interactiveQuestionReadStore{},
		interactive: &interactiveLifecycleStore{},
		events:      mustQuestionRecorder(t),
		safe:        interactiveSafeContext(),
	}
	if _, err := q.Open(context.Background(), "key", engine.QuestionRequest{Prompt: "Question", Kind: "TEXT", Blocking: false}); err == nil {
		t.Fatal("non-blocking interactive Question unexpectedly accepted")
	}
	if _, err := q.Open(context.Background(), "", engine.QuestionRequest{Prompt: "Question", Kind: "TEXT", Blocking: true}); err == nil {
		t.Fatal("empty correlation key unexpectedly accepted")
	}
	if _, err := q.WaitAnswer(context.Background(), ""); err == nil {
		t.Fatal("empty Question id unexpectedly accepted")
	}
	if err := (&interactiveQuestioner{}).Resolve(context.Background(), "question-1"); err == nil {
		t.Fatal("Resolve() without capability unexpectedly succeeded")
	}
}

func interactiveSafeContext() executioncontext.SafeContext {
	return executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project-1"},
		Issue:     executioncontext.IssueContext{ID: "issue-1"},
		Run:       executioncontext.RunContext{ID: "run-1"},
		Agent:     executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
	}
}

func mustQuestionRecorder(t *testing.T) *evidence.Recorder {
	t.Helper()
	recorder, err := evidence.NewRecorder(&questionEventStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return recorder
}

var _ store.QuestionStore = (*interactiveQuestionReadStore)(nil)
var _ store.InteractiveQuestionStore = (*interactiveLifecycleStore)(nil)
