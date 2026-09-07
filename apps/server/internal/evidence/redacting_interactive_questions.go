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

func (s *RedactingStore) interactiveQuestionStore() (store.InteractiveQuestionStore, error) {
	if s == nil || !store.SupportsInteractiveQuestionStore(s.ControlPlaneStore) {
		return nil, fmt.Errorf("redacting store base does not support interactive Question operations")
	}
	return s.ControlPlaneStore.(store.InteractiveQuestionStore), nil
}

func (s *RedactingStore) OpenInteractiveQuestion(ctx context.Context, input store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionResult, error) {
	questions, err := s.interactiveQuestionStore()
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}
	question := input.Question
	question.Prompt = s.registry.RedactString(question.RunID, question.Prompt)
	if question.Recommendation != nil {
		value := s.registry.RedactString(question.RunID, *question.Recommendation)
		question.Recommendation = &value
	}
	if len(question.Options) != 0 {
		redacted, redactErr := s.registry.RedactJSON(question.RunID, question.Options)
		if redactErr != nil {
			return store.OpenInteractiveQuestionResult{}, redactErr
		}
		question.Options = redacted
	}
	input.Question = question
	return questions.OpenInteractiveQuestion(ctx, input)
}

func (s *RedactingStore) ResolveInteractiveQuestion(ctx context.Context, projectID, questionID string) (store.ResolveInteractiveQuestionResult, error) {
	questions, err := s.interactiveQuestionStore()
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	return questions.ResolveInteractiveQuestion(ctx, projectID, questionID)
}

var _ store.InteractiveQuestionStore = (*RedactingStore)(nil)
var _ store.InteractiveQuestionStoreCapability = (*RedactingStore)(nil)
