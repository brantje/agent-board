package evidence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) SupportsQuestionStore() bool {
	if s == nil {
		return false
	}
	return store.SupportsQuestionStore(s.ControlPlaneStore)
}

func (s *RedactingStore) questionStore() (store.QuestionStore, error) {
	if s == nil || !store.SupportsQuestionStore(s.ControlPlaneStore) {
		return nil, fmt.Errorf("redacting store base does not support Question operations")
	}
	return s.ControlPlaneStore.(store.QuestionStore), nil
}

func (s *RedactingStore) CreateQuestion(ctx context.Context, input store.Question) (store.Question, error) {
	questions, err := s.questionStore()
	if err != nil {
		return store.Question{}, err
	}
	input.Prompt = s.registry.RedactString(input.RunID, input.Prompt)
	if input.Recommendation != nil {
		value := s.registry.RedactString(input.RunID, *input.Recommendation)
		input.Recommendation = &value
	}
	if len(input.Options) != 0 {
		redacted, redactErr := s.registry.RedactJSON(input.RunID, input.Options)
		if redactErr != nil {
			return store.Question{}, redactErr
		}
		input.Options = redacted
	}
	return questions.CreateQuestion(ctx, input)
}

func (s *RedactingStore) GetQuestion(ctx context.Context, projectID, questionID string) (store.Question, error) {
	questions, err := s.questionStore()
	if err != nil {
		return store.Question{}, err
	}
	return questions.GetQuestion(ctx, projectID, questionID)
}

func (s *RedactingStore) ListQuestions(ctx context.Context, projectID string, filter store.QuestionFilter) ([]store.Question, error) {
	questions, err := s.questionStore()
	if err != nil {
		return nil, err
	}
	return questions.ListQuestions(ctx, projectID, filter)
}

func (s *RedactingStore) GetDecisionByQuestion(ctx context.Context, projectID, questionID string) (store.Decision, error) {
	questions, err := s.questionStore()
	if err != nil {
		return store.Decision{}, err
	}
	return questions.GetDecisionByQuestion(ctx, projectID, questionID)
}

func (s *RedactingStore) GetOpenBlockingQuestion(ctx context.Context, projectID, runID string) (store.Question, error) {
	questions, err := s.questionStore()
	if err != nil {
		return store.Question{}, err
	}
	return questions.GetOpenBlockingQuestion(ctx, projectID, runID)
}

func (s *RedactingStore) AnswerQuestion(ctx context.Context, input store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	questions, err := s.questionStore()
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}
	question, err := questions.GetQuestion(ctx, input.ProjectID, input.QuestionID)
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}
	if input.Answer.Text != nil {
		value := s.registry.RedactString(question.RunID, *input.Answer.Text)
		input.Answer.Text = &value
	}
	if len(input.Answer.OptionIDs) != 0 {
		encoded, marshalErr := json.Marshal(input.Answer.OptionIDs)
		if marshalErr != nil {
			return store.AnswerQuestionResult{}, marshalErr
		}
		redacted, redactErr := s.registry.RedactJSON(question.RunID, encoded)
		if redactErr != nil {
			return store.AnswerQuestionResult{}, redactErr
		}
		if unmarshalErr := json.Unmarshal(redacted, &input.Answer.OptionIDs); unmarshalErr != nil {
			return store.AnswerQuestionResult{}, unmarshalErr
		}
	}
	return questions.AnswerQuestion(ctx, input)
}

var _ store.QuestionStore = (*RedactingStore)(nil)
var _ store.QuestionStoreCapability = (*RedactingStore)(nil)
