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

type orchestrationQuestionStore struct {
	*processTestStore
	questions       []store.Question
	decision        store.Decision
	listErr         error
	decisionErr     error
	openErr         error
	createErr       error
	created         []store.Question
}

func (s *orchestrationQuestionStore) CreateQuestion(_ context.Context, question store.Question) (store.Question, error) {
	if s.createErr != nil {
		return store.Question{}, s.createErr
	}
	question.ID = "created-question"
	s.created = append(s.created, question)
	return question, nil
}
func (s *orchestrationQuestionStore) GetQuestion(context.Context, string, string) (store.Question, error) {
	return store.Question{}, store.ErrNotFound
}
func (s *orchestrationQuestionStore) ListQuestions(context.Context, string, store.QuestionFilter) ([]store.Question, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]store.Question(nil), s.questions...), nil
}
func (s *orchestrationQuestionStore) GetDecisionByQuestion(context.Context, string, string) (store.Decision, error) {
	if s.decisionErr != nil {
		return store.Decision{}, s.decisionErr
	}
	return s.decision, nil
}
func (s *orchestrationQuestionStore) GetOpenBlockingQuestion(context.Context, string, string) (store.Question, error) {
	if s.openErr != nil {
		return store.Question{}, s.openErr
	}
	return store.Question{}, store.ErrNotFound
}
func (s *orchestrationQuestionStore) AnswerQuestion(context.Context, store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	return store.AnswerQuestionResult{}, nil
}

type acquiringRuntime struct {
	*processTestRuntime
	instance store.RuntimeInstance
	calls    int
}

func (r *acquiringRuntime) Acquire(_ context.Context, projectID, _, runtimeID string) (store.RuntimeInstance, error) {
	r.calls++
	if r.instance.ID == "" {
		r.instance = store.RuntimeInstance{ID: "acquired-runtime", ProjectID: projectID, RuntimeID: runtimeID, Status: "RUNNING"}
	}
	return r.instance, nil
}

func continuationSafeContext() executioncontext.SafeContext {
	return executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue: executioncontext.IssueContext{ID: "issue-1"},
		Run: executioncontext.RunContext{ID: "run-1"},
		Agent: executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
	}
}

func TestEngineRequestLoadsLatestBlockingContinuation(t *testing.T) {
	safe := continuationSafeContext()
	questionID, runID := "blocking-question", safe.Run.ID
	storeWithQuestions := &orchestrationQuestionStore{
		processTestStore: &processTestStore{},
		questions: []store.Question{
			{ID: "nonblocking", ProjectID: safe.Project.ID, RunID: runID, Blocking: false, Status: "ANSWERED"},
			{ID: questionID, ProjectID: safe.Project.ID, RunID: runID, Prompt: "Proceed?", Blocking: true, Status: "ANSWERED"},
		},
		decision: store.Decision{
			ID: "decision-1", ProjectID: safe.Project.ID, QuestionID: &questionID, RunID: &runID,
			SafeDetails: json.RawMessage(`{"questionAnswer":{"kind":"TEXT","text":"yes"}}`),
		},
	}
	recorder, err := evidence.NewRecorder(storeWithQuestions, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{store: storeWithQuestions, events: recorder}
	request, err := processor.engineRequest(context.Background(), safe, nil, "runtime-instance-1")
	if err != nil {
		t.Fatal(err)
	}
	if request.Questions == nil || request.Continuation == nil {
		t.Fatalf("request=%+v", request)
	}
	if request.Continuation.QuestionID != questionID || request.Continuation.DecisionID != "decision-1" || request.Continuation.Answer.Text == nil || *request.Continuation.Answer.Text != "yes" {
		t.Fatalf("continuation=%+v", request.Continuation)
	}
}

func TestEngineRequestWithoutQuestionStoreHasNoContinuation(t *testing.T) {
	processor := &Processor{store: &processTestStore{}}
	request, err := processor.engineRequest(context.Background(), continuationSafeContext(), nil, "runtime-instance-1")
	if err != nil {
		t.Fatal(err)
	}
	if request.Questions != nil || request.Continuation != nil {
		t.Fatalf("request=%+v", request)
	}
}

func TestLoadContinuationRejectsInvalidDurableState(t *testing.T) {
	safe := continuationSafeContext()
	questionID, runID := "question-1", safe.Run.ID
	question := store.Question{ID: questionID, ProjectID: safe.Project.ID, RunID: runID, Blocking: true, Status: "ANSWERED"}

	cases := []struct {
		name  string
		store *orchestrationQuestionStore
	}{
		{name: "list failure", store: &orchestrationQuestionStore{listErr: errors.New("list failed")}},
		{name: "missing decision", store: &orchestrationQuestionStore{questions: []store.Question{question}, decisionErr: store.ErrNotFound}},
		{name: "bad binding", store: &orchestrationQuestionStore{questions: []store.Question{question}, decision: store.Decision{ID: "decision", QuestionID: &questionID}}},
		{name: "bad details", store: &orchestrationQuestionStore{questions: []store.Question{question}, decision: store.Decision{ID: "decision", QuestionID: &questionID, RunID: &runID, SafeDetails: json.RawMessage(`{`)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if continuation, err := loadContinuation(context.Background(), tc.store, safe); err == nil || continuation != nil {
				t.Fatalf("continuation=%+v err=%v", continuation, err)
			}
		})
	}

	none, err := loadContinuation(context.Background(), &orchestrationQuestionStore{questions: []store.Question{{ID: "nonblocking", RunID: runID, Status: "ANSWERED"}}}, safe)
	if err != nil || none != nil {
		t.Fatalf("nonblocking continuation=%+v err=%v", none, err)
	}
}

func TestAcquireRuntimeUsesOptionalAcquirerAndFallback(t *testing.T) {
	acquirer := &acquiringRuntime{processTestRuntime: &processTestRuntime{}}
	processor := &Processor{runtimes: acquirer}
	instance, err := processor.acquireRuntime(context.Background(), "project-1", "issue-1", "runtime-1")
	if err != nil || instance.ID != "acquired-runtime" || acquirer.calls != 1 || acquirer.created != 0 {
		t.Fatalf("acquire instance=%+v calls=%d created=%d err=%v", instance, acquirer.calls, acquirer.created, err)
	}

	fallback := &processTestRuntime{}
	processor.runtimes = fallback
	instance, err = processor.acquireRuntime(context.Background(), "project-1", "issue-1", "runtime-1")
	if err != nil || fallback.created != 1 || instance.ID != "runtime-instance" {
		t.Fatalf("fallback instance=%+v created=%d err=%v", instance, fallback.created, err)
	}
}

func TestQuestionerErrorAndNonBlockingPaths(t *testing.T) {
	if _, err := (*questioner)(nil).Ask(context.Background(), engine.QuestionRequest{Prompt: "Explain", Kind: "TEXT"}); err == nil {
		t.Fatal("expected unavailable Question capability error")
	}

	safe := continuationSafeContext()
	events := &questionEventStore{}
	recorder, err := evidence.NewRecorder(events, nil)
	if err != nil {
		t.Fatal(err)
	}
	questionStore := &orchestrationQuestionStore{processTestStore: &processTestStore{}}
	q := &questioner{store: questionStore, events: recorder, safe: safe, runtimeInstanceID: "runtime-instance-1"}

	if _, err := q.Ask(context.Background(), engine.QuestionRequest{Prompt: " ", Kind: "TEXT"}); err == nil {
		t.Fatal("expected validation error")
	}
	questionStore.openErr = errors.New("lookup failed")
	if _, err := q.Ask(context.Background(), engine.QuestionRequest{Prompt: "Continue?", Kind: "TEXT", Blocking: true}); err == nil || err.Error() != "lookup failed" {
		t.Fatalf("blocking lookup error=%v", err)
	}
	questionStore.openErr = store.ErrNotFound
	questionStore.createErr = errors.New("create failed")
	if _, err := q.Ask(context.Background(), engine.QuestionRequest{Prompt: "Continue?", Kind: "TEXT", Blocking: true}); err == nil || err.Error() != "create failed" {
		t.Fatalf("create error=%v", err)
	}
	questionStore.createErr = nil
	questionStore.openErr = nil
	nonblocking, err := q.Ask(context.Background(), engine.QuestionRequest{Prompt: "FYI?", Kind: "TEXT", Blocking: false})
	if err != nil || nonblocking.ID != "created-question" || nonblocking.Blocking || len(questionStore.created) != 1 {
		t.Fatalf("nonblocking=%+v created=%+v err=%v", nonblocking, questionStore.created, err)
	}
	if len(events.events) != 1 || events.events[0].Type != "question.created" {
		t.Fatalf("events=%+v", events.events)
	}
}

func TestValidateQuestionRequestRejectsMissingOptionFieldsAndUnknownKind(t *testing.T) {
	for _, request := range []engine.QuestionRequest{
		{Prompt: "Choose", Kind: "SINGLE_CHOICE", Options: []engine.QuestionOption{{ID: "", Label: "A"}}},
		{Prompt: "Choose", Kind: "MULTI_CHOICE", Options: []engine.QuestionOption{{ID: "a", Label: " "}}},
		{Prompt: "Choose", Kind: "UNKNOWN"},
	} {
		if err := validateQuestionRequest(request); err == nil {
			t.Fatalf("request=%+v unexpectedly valid", request)
		}
	}
}
