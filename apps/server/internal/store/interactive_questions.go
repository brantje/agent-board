package store

import (
	"context"
	"time"
)

const (
	InteractiveQuestionOpen      = "OPEN"
	InteractiveQuestionAnswered  = "ANSWERED"
	InteractiveQuestionResolved  = "RESOLVED"
	InteractiveQuestionCancelled = "CANCELLED"
)

type InteractiveQuestionBinding struct {
	QuestionID     string
	ProjectID      string
	RunID          string
	Engine         string
	CorrelationKey string
	State          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type OpenInteractiveQuestionCommand struct {
	Question          Question
	Engine            string
	CorrelationKey    string
	RuntimeInstanceID string
}

type OpenInteractiveQuestionResult struct {
	Question       Question
	Binding        InteractiveQuestionBinding
	Run            Run
	Created        bool
	EnteredWaiting bool
	Events         []Event
}

type OpenInteractiveQuestionsResult struct {
	Questions      []OpenInteractiveQuestionResult
	Run            Run
	EnteredWaiting bool
	Events         []Event
}

type ResolveInteractiveQuestionResult struct {
	Binding InteractiveQuestionBinding
	Run     Run
	Resumed bool
	Events  []Event
}

type InteractiveQuestionStore interface {
	OpenInteractiveQuestion(context.Context, OpenInteractiveQuestionCommand) (OpenInteractiveQuestionResult, error)
	ResolveInteractiveQuestion(context.Context, string, string) (ResolveInteractiveQuestionResult, error)
}

// InteractiveQuestionBatchStore atomically creates or reconciles a group of
// interactive Questions that belong to one native request. Either the complete
// batch and its required durable lifecycle evidence commit together, or none of
// the new state does. Implementations must return result.Questions in the same
// order as the input commands so callers preserve correlation-key ordering.
type InteractiveQuestionBatchStore interface {
	OpenInteractiveQuestions(context.Context, []OpenInteractiveQuestionCommand) (OpenInteractiveQuestionsResult, error)
}

type InteractiveQuestionStoreCapability interface {
	SupportsInteractiveQuestionStore() bool
}

type InteractiveQuestionBatchStoreCapability interface {
	SupportsInteractiveQuestionBatchStore() bool
}

func SupportsInteractiveQuestionStore(value any) bool {
	if value == nil {
		return false
	}
	if _, ok := value.(InteractiveQuestionStore); !ok {
		return false
	}
	if capability, ok := value.(InteractiveQuestionStoreCapability); ok {
		return capability.SupportsInteractiveQuestionStore()
	}
	return true
}

func SupportsInteractiveQuestionBatchStore(value any) bool {
	if value == nil {
		return false
	}
	if _, ok := value.(InteractiveQuestionBatchStore); !ok {
		return false
	}
	if capability, ok := value.(InteractiveQuestionBatchStoreCapability); ok {
		return capability.SupportsInteractiveQuestionBatchStore()
	}
	return true
}
