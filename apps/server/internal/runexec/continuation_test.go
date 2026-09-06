package runexec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type continuationStore struct {
	questions []store.Question
	decisions map[string]store.Decision
}

func (s *continuationStore) CreateQuestion(context.Context, store.Question) (store.Question, error) {
	return store.Question{}, nil
}
func (s *continuationStore) GetQuestion(context.Context, string, string) (store.Question, error) {
	return store.Question{}, store.ErrNotFound
}
func (s *continuationStore) ListQuestions(_ context.Context, _ string, filter store.QuestionFilter) ([]store.Question, error) {
	if filter.RunID == nil || len(filter.Statuses) != 1 || filter.Statuses[0] != "ANSWERED" {
		return nil, nil
	}
	return append([]store.Question(nil), s.questions...), nil
}
func (s *continuationStore) GetDecisionByQuestion(_ context.Context, _ string, questionID string) (store.Decision, error) {
	decision, ok := s.decisions[questionID]
	if !ok {
		return store.Decision{}, store.ErrNotFound
	}
	return decision, nil
}
func (s *continuationStore) GetOpenBlockingQuestion(context.Context, string, string) (store.Question, error) {
	return store.Question{}, store.ErrNotFound
}
func (s *continuationStore) AnswerQuestion(context.Context, store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	return store.AnswerQuestionResult{}, nil
}

func TestLoadContinuationUsesLatestAnsweredBlockingQuestionOnly(t *testing.T) {
	runID := "run-1"
	oldID := "question-old"
	latestID := "question-latest"
	oldDetails, _ := json.Marshal(map[string]any{"questionAnswer": store.QuestionAnswer{Kind: "TEXT", Text: ptr("old")}})
	latestDetails, _ := json.Marshal(map[string]any{"questionAnswer": store.QuestionAnswer{Kind: "SINGLE_CHOICE", OptionIDs: []string{"safe"}}})
	questions := &continuationStore{
		questions: []store.Question{
			{ID: oldID, ProjectID: "project-1", RunID: runID, Prompt: "Old", Blocking: true, Status: "ANSWERED"},
			{ID: "non-blocking", ProjectID: "project-1", RunID: runID, Prompt: "FYI", Blocking: false, Status: "ANSWERED"},
			{ID: latestID, ProjectID: "project-1", RunID: runID, Prompt: "Latest", Blocking: true, Status: "ANSWERED"},
		},
		decisions: map[string]store.Decision{
			oldID:    {ID: "decision-old", ProjectID: "project-1", RunID: &runID, QuestionID: &oldID, SafeDetails: oldDetails},
			latestID: {ID: "decision-latest", ProjectID: "project-1", RunID: &runID, QuestionID: &latestID, SafeDetails: latestDetails},
		},
	}

	continuation, err := loadContinuation(context.Background(), questions, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run:     executioncontext.RunContext{ID: runID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil {
		t.Fatal("expected continuation")
	}
	if continuation.QuestionID != latestID || continuation.DecisionID != "decision-latest" || continuation.Prompt != "Latest" {
		t.Fatalf("continuation=%+v", continuation)
	}
	if continuation.Answer.Kind != "SINGLE_CHOICE" || len(continuation.Answer.OptionIDs) != 1 || continuation.Answer.OptionIDs[0] != "safe" {
		t.Fatalf("answer=%+v", continuation.Answer)
	}
}

func ptr(value string) *string { return &value }
