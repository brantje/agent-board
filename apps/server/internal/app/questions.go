package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type QuestionService struct {
	store store.QuestionStore
}

func NewQuestionService(questionStore store.QuestionStore) (*QuestionService, error) {
	if questionStore == nil {
		return nil, fmt.Errorf("Question store is required")
	}
	return &QuestionService{store: questionStore}, nil
}

func (s *QuestionService) List(ctx context.Context, projectID string, filter store.QuestionFilter) ([]store.Question, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, NewError("invalid_argument", "projectId is required", store.ErrInvalidArgument)
	}
	questions, err := s.store.ListQuestions(ctx, projectID, filter)
	if err != nil {
		return nil, translateStoreError(err, "question")
	}
	return questions, nil
}

func (s *QuestionService) Get(ctx context.Context, projectID, questionID string) (store.Question, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(questionID) == "" {
		return store.Question{}, NewError("invalid_argument", "projectId and questionId are required", store.ErrInvalidArgument)
	}
	question, err := s.store.GetQuestion(ctx, projectID, questionID)
	if err != nil {
		return store.Question{}, translateStoreError(err, "question")
	}
	return question, nil
}

func (s *QuestionService) Answer(ctx context.Context, projectID, questionID string, answer store.QuestionAnswer, actorID *string) (store.AnswerQuestionResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(questionID) == "" {
		return store.AnswerQuestionResult{}, NewError("invalid_argument", "projectId and questionId are required", store.ErrInvalidArgument)
	}
	result, err := s.store.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID:  projectID,
		QuestionID: questionID,
		Answer:     answer,
		ActorType:  "HUMAN",
		ActorID:    actorID,
	})
	if err != nil {
		return store.AnswerQuestionResult{}, translateStoreError(err, "question")
	}
	return result, nil
}
