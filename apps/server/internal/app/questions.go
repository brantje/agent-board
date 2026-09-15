package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type QuestionService struct {
	store     store.QuestionStore
	publisher persistedEventPublisher
}

func NewQuestionService(questionStore store.QuestionStore) (*QuestionService, error) {
	if questionStore == nil {
		return nil, fmt.Errorf("question store is required")
	}
	return &QuestionService{store: questionStore}, nil
}

func (s *QuestionService) SetPersistedEventPublisher(publisher persistedEventPublisher) {
	if s == nil {
		return
	}
	s.publisher = publisher
}

func (s *QuestionService) List(ctx context.Context, projectID string, filter store.QuestionFilter) ([]store.Question, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, NewError("invalid_argument", "projectId is required", store.ErrInvalidArgument)
	}
	for _, status := range filter.Statuses {
		switch status {
		case "OPEN", "ANSWERED", "CANCELLED":
		default:
			return nil, NewError("invalid_argument", "status must be OPEN, ANSWERED or CANCELLED", store.ErrInvalidArgument)
		}
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
		ActorType:  store.ActorTypeHuman,
		ActorID:    actorID,
	})
	if err != nil {
		return store.AnswerQuestionResult{}, translateStoreError(err, "question")
	}
	publishPersistedEvents(ctx, s.publisher, result.Events)
	return result, nil
}
