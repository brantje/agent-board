package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

type replyFinalizationQuestioner struct {
	correlations []string
	accepted     []engine.AcceptedInteractiveQuestionReply
	resolved     map[string]bool
	resolveCalls map[string]int
}

func (q *replyFinalizationQuestioner) Open(ctx context.Context, correlation string, request engine.QuestionRequest) (engine.Question, error) {
	opened, err := q.OpenBatch(ctx, []engine.CorrelatedQuestionRequest{{CorrelationKey: correlation, Question: request}})
	if err != nil {
		return engine.Question{}, err
	}
	return opened[0], nil
}

func (q *replyFinalizationQuestioner) OpenBatch(_ context.Context, requests []engine.CorrelatedQuestionRequest) ([]engine.Question, error) {
	q.correlations = make([]string, len(requests))
	opened := make([]engine.Question, len(requests))
	for index, request := range requests {
		q.correlations[index] = request.CorrelationKey
		opened[index] = engine.Question{ID: fmt.Sprintf("canonical-%d", index), Blocking: true}
	}
	if q.resolved == nil {
		q.resolved = make(map[string]bool)
	}
	if q.resolveCalls == nil {
		q.resolveCalls = make(map[string]int)
	}
	return opened, nil
}

func (q *replyFinalizationQuestioner) WaitAnswer(_ context.Context, questionID string) (engine.QuestionAnswer, error) {
	answer := "answer-" + questionID
	return engine.QuestionAnswer{Kind: "TEXT", Text: &answer}, nil
}

func (q *replyFinalizationQuestioner) Resolve(_ context.Context, questionID string) error {
	q.resolveCalls[questionID]++
	if questionID == "canonical-1" && q.resolveCalls[questionID] == 1 {
		return fmt.Errorf("injected later Resolve failure")
	}
	q.resolved[questionID] = true
	return nil
}

func (q *replyFinalizationQuestioner) MarkReplyAccepted(_ context.Context, accepted []engine.AcceptedInteractiveQuestionReply) error {
	q.accepted = append([]engine.AcceptedInteractiveQuestionReply(nil), accepted...)
	return nil
}

func (q *replyFinalizationQuestioner) ListReplyAccepted(_ context.Context) ([]engine.AcceptedInteractiveQuestionReply, error) {
	pending := make([]engine.AcceptedInteractiveQuestionReply, 0, len(q.accepted))
	for _, binding := range q.accepted {
		if !q.resolved[binding.QuestionID] {
			pending = append(pending, binding)
		}
	}
	return pending, nil
}

func TestAcceptedNativeReplyRetriesLaterBindingResolutionAfterRequestDisappears(t *testing.T) {
	replied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/session/ses_1/question/que_1/reply":
			replied = true
			w.WriteHeader(http.StatusNoContent)
		case "GET /api/session/ses_1/question":
			if !replied {
				t.Error("pending list queried before native reply was accepted")
			}
			writeNativeJSON(t, w, map[string]any{"data": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	native := mustNativeQuestionClient(t, server)
	questions := &replyFinalizationQuestioner{}
	state := newRunState("ses_1", questions, nil)
	request := clientQuestionRequestForFinalization()

	if err := state.handleQuestion(context.Background(), native, request); err != nil {
		t.Fatalf("handleQuestion() error=%v", err)
	}
	if len(questions.accepted) != 2 {
		t.Fatalf("accepted bindings=%v want 2", questions.accepted)
	}
	if len(questions.resolveCalls) != 0 {
		t.Fatalf("bindings resolved before native request absence was confirmed: %v", questions.resolveCalls)
	}

	hadPending, err := reconcilePendingQuestions(context.Background(), native, "ses_1", state)
	if err != nil {
		t.Fatalf("first reconciliation error=%v", err)
	}
	if !hadPending {
		t.Fatal("first reconciliation did not retain unresolved accepted bindings")
	}
	if !questions.resolved["canonical-0"] || questions.resolved["canonical-1"] {
		t.Fatalf("resolution state after injected failure=%v", questions.resolved)
	}
	if questions.resolveCalls["canonical-1"] != 1 {
		t.Fatalf("later binding resolve calls=%d want 1", questions.resolveCalls["canonical-1"])
	}

	hadPending, err = reconcilePendingQuestions(context.Background(), native, "ses_1", state)
	if err != nil {
		t.Fatalf("second reconciliation error=%v", err)
	}
	if !hadPending {
		t.Fatal("second reconciliation should report the binding it just retried")
	}
	if !questions.resolved["canonical-1"] || questions.resolveCalls["canonical-1"] != 2 {
		t.Fatalf("later binding was not recovered: resolved=%v calls=%v", questions.resolved, questions.resolveCalls)
	}

	hadPending, err = reconcilePendingQuestions(context.Background(), native, "ses_1", state)
	if err != nil {
		t.Fatalf("settled reconciliation error=%v", err)
	}
	if hadPending {
		t.Fatal("accepted reply remained pending after every binding resolved")
	}
}

func clientQuestionRequestForFinalization() client.QuestionRequest {
	return client.QuestionRequest{
		ID:        "que_1",
		SessionID: "ses_1",
		Questions: []client.QuestionInfo{
			{Question: "First answer?"},
			{Question: "Second answer?"},
		},
	}
}
