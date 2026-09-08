package evidence

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) SupportsInteractiveQuestionStore() bool {
	if s == nil {
		return false
	}
	return store.SupportsInteractiveQuestionStore(s.ControlPlaneStore)
}

func (s *RedactingStore) SupportsInteractiveQuestionBatchStore() bool {
	if s == nil {
		return false
	}
	return store.SupportsInteractiveQuestionBatchStore(s.ControlPlaneStore)
}

func (s *RedactingStore) interactiveQuestionStore() (store.InteractiveQuestionStore, error) {
	if s == nil || !store.SupportsInteractiveQuestionStore(s.ControlPlaneStore) {
		return nil, fmt.Errorf("redacting store base does not support interactive Question operations")
	}
	return s.ControlPlaneStore.(store.InteractiveQuestionStore), nil
}

func (s *RedactingStore) interactiveQuestionBatchStore() (store.InteractiveQuestionBatchStore, error) {
	if s == nil || !store.SupportsInteractiveQuestionBatchStore(s.ControlPlaneStore) {
		return nil, fmt.Errorf("redacting store base does not support interactive Question batch operations")
	}
	return s.ControlPlaneStore.(store.InteractiveQuestionBatchStore), nil
}

func (s *RedactingStore) OpenInteractiveQuestion(ctx context.Context, input store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionResult, error) {
	questions, err := s.interactiveQuestionStore()
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}
	redacted, err := s.redactInteractiveQuestionCommand(input)
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}
	return questions.OpenInteractiveQuestion(ctx, redacted)
}

func (s *RedactingStore) OpenInteractiveQuestions(ctx context.Context, inputs []store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionsResult, error) {
	questions, err := s.interactiveQuestionBatchStore()
	if err != nil {
		return store.OpenInteractiveQuestionsResult{}, err
	}
	redacted := make([]store.OpenInteractiveQuestionCommand, len(inputs))
	for index, input := range inputs {
		command, redactErr := s.redactInteractiveQuestionCommand(input)
		if redactErr != nil {
			return store.OpenInteractiveQuestionsResult{}, redactErr
		}
		redacted[index] = command
	}
	return questions.OpenInteractiveQuestions(ctx, redacted)
}

func (s *RedactingStore) redactInteractiveQuestionCommand(input store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionCommand, error) {
	question := input.Question
	question.Prompt = s.registry.RedactString(question.RunID, question.Prompt)
	if question.Recommendation != nil {
		value := s.registry.RedactString(question.RunID, *question.Recommendation)
		question.Recommendation = &value
	}
	if len(question.Options) != 0 {
		redacted, err := s.registry.RedactJSON(question.RunID, question.Options)
		if err != nil {
			return store.OpenInteractiveQuestionCommand{}, err
		}
		question.Options = redacted
	}
	input.Question = question
	return input, nil
}

func (s *RedactingStore) ResolveInteractiveQuestion(ctx context.Context, projectID, questionID string) (store.ResolveInteractiveQuestionResult, error) {
	questions, err := s.interactiveQuestionStore()
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	return questions.ResolveInteractiveQuestion(ctx, projectID, questionID)
}

var _ store.InteractiveQuestionStore = (*RedactingStore)(nil)
var _ store.InteractiveQuestionBatchStore = (*RedactingStore)(nil)
var _ store.InteractiveQuestionStoreCapability = (*RedactingStore)(nil)
var _ store.InteractiveQuestionBatchStoreCapability = (*RedactingStore)(nil)
