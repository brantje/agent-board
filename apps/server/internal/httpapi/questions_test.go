package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type apiQuestionStore struct {
	questions []store.Question
	answered  *store.AnswerQuestionCommand
}

func (s *apiQuestionStore) CreateQuestion(context.Context, store.Question) (store.Question, error) {
	return store.Question{}, nil
}
func (s *apiQuestionStore) GetQuestion(_ context.Context, projectID, id string) (store.Question, error) {
	for _, question := range s.questions {
		if question.ProjectID == projectID && question.ID == id {
			return question, nil
		}
	}
	return store.Question{}, store.ErrNotFound
}
func (s *apiQuestionStore) ListQuestions(context.Context, string, store.QuestionFilter) ([]store.Question, error) {
	return append([]store.Question(nil), s.questions...), nil
}
func (s *apiQuestionStore) GetDecisionByQuestion(context.Context, string, string) (store.Decision, error) {
	return store.Decision{}, store.ErrNotFound
}
func (s *apiQuestionStore) GetOpenBlockingQuestion(context.Context, string, string) (store.Question, error) {
	return store.Question{}, store.ErrNotFound
}
func (s *apiQuestionStore) AnswerQuestion(_ context.Context, command store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	s.answered = &command
	question := s.questions[0]
	question.Status = "ANSWERED"
	job := store.SchedulerJob{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", ProjectID: projectID, RunID: runID, Kind: "RESUME", State: "QUEUED"}
	return store.AnswerQuestionResult{
		Question: question,
		Decision: store.Decision{ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", ProjectID: projectID},
		Run:      store.Run{ID: runID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, Status: "QUEUED"},
		Job:      &job,
	}, nil
}

func TestQuestionRoutesListAndAnswer(t *testing.T) {
	questionStore := &apiQuestionStore{questions: []store.Question{{
		ID:        otherID,
		ProjectID: projectID,
		IssueID:   issueID,
		RunID:     runID,
		Prompt:    "Which strategy?",
		Kind:      "TEXT",
		Options:   json.RawMessage(`[]`),
		Blocking:  true,
		Status:    "OPEN",
	}}}
	questionService, err := app.NewQuestionService(questionStore)
	if err != nil {
		t.Fatal(err)
	}
	router := newRouter(app.New(&fakeControlPlaneStore{}), nil, nil, nil, questionService)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/questions?runId="+runID+"&status=OPEN", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "Which strategy?") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	answer := httptest.NewRecorder()
	router.ServeHTTP(answer, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/questions/"+otherID+"/answer", strings.NewReader(`{"kind":"TEXT","text":"Use the safe path"}`)))
	if answer.Code != http.StatusOK {
		t.Fatalf("answer status=%d body=%s", answer.Code, answer.Body.String())
	}
	if questionStore.answered == nil || questionStore.answered.ActorType != "HUMAN" || questionStore.answered.Answer.Text == nil || *questionStore.answered.Answer.Text != "Use the safe path" {
		t.Fatalf("answer command=%+v", questionStore.answered)
	}
	if !strings.Contains(answer.Body.String(), "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb") {
		t.Fatalf("answer response=%s", answer.Body.String())
	}
}
