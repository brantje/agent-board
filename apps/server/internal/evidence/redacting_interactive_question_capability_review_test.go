package evidence

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type interactiveQuestionBatchRedactionStore struct {
	interactiveQuestionRedactionStore
}

func (s *interactiveQuestionBatchRedactionStore) OpenInteractiveQuestions(_ context.Context, inputs []store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionsResult, error) {
	results := make([]store.OpenInteractiveQuestionResult, len(inputs))
	for index, input := range inputs {
		input.Question.ID = "question-batch"
		results[index] = store.OpenInteractiveQuestionResult{Question: input.Question}
	}
	return store.OpenInteractiveQuestionsResult{Questions: results}, nil
}

func TestRedactingStoreForwardsInteractiveBatchCapability(t *testing.T) {
	withoutBatch := NewRedactingStore(&interactiveQuestionRedactionStore{}, redaction.NewRegistry())
	if store.SupportsInteractiveQuestionBatchStore(withoutBatch) {
		t.Fatal("redacting store reported batch support when wrapped store lacks it")
	}

	withBatch := NewRedactingStore(&interactiveQuestionBatchRedactionStore{}, redaction.NewRegistry())
	if !store.SupportsInteractiveQuestionBatchStore(withBatch) {
		t.Fatal("redacting store did not preserve wrapped batch support")
	}
}

var _ store.InteractiveQuestionBatchStore = (*interactiveQuestionBatchRedactionStore)(nil)
