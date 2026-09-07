package evidence

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type redactingInteractiveBatchBase struct {
	store.ControlPlaneStore
	commands []store.OpenInteractiveQuestionCommand
}

func (b *redactingInteractiveBatchBase) OpenInteractiveQuestions(_ context.Context, commands []store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionsResult, error) {
	b.commands = append([]store.OpenInteractiveQuestionCommand(nil), commands...)
	return store.OpenInteractiveQuestionsResult{}, nil
}

func TestRedactingStoreRedactsInteractiveQuestionBatchBeforePersistence(t *testing.T) {
	registry := redaction.NewRegistry()
	registry.Register("run-1", []string{"super-secret"})
	base := &redactingInteractiveBatchBase{}
	redacting := NewRedactingStore(base, registry)
	recommendation := "prefer super-secret"
	options := json.RawMessage(`[{"id":"option-1","label":"super-secret choice"}]`)

	_, err := redacting.OpenInteractiveQuestions(context.Background(), []store.OpenInteractiveQuestionCommand{{
		Question: store.Question{
			ProjectID:      "project-1",
			IssueID:        "issue-1",
			RunID:          "run-1",
			Prompt:         "prompt super-secret",
			Kind:           "SINGLE_CHOICE",
			Options:        options,
			Recommendation: &recommendation,
			Blocking:       true,
			Status:         "OPEN",
		},
		Engine:         "opencode",
		CorrelationKey: "ses/request/0",
	}})
	if err != nil {
		t.Fatalf("OpenInteractiveQuestions() error=%v", err)
	}
	if len(base.commands) != 1 {
		t.Fatalf("commands=%d want 1", len(base.commands))
	}
	persisted := base.commands[0]
	if strings.Contains(persisted.Question.Prompt, "super-secret") {
		t.Fatalf("prompt was not redacted: %q", persisted.Question.Prompt)
	}
	if persisted.Question.Recommendation == nil || strings.Contains(*persisted.Question.Recommendation, "super-secret") {
		t.Fatalf("recommendation was not redacted: %v", persisted.Question.Recommendation)
	}
	if strings.Contains(string(persisted.Question.Options), "super-secret") {
		t.Fatalf("options were not redacted: %s", persisted.Question.Options)
	}
	if persisted.CorrelationKey != "ses/request/0" || persisted.Engine != "opencode" {
		t.Fatalf("non-sensitive binding metadata changed: %+v", persisted)
	}
}

func TestRedactingStoreRejectsInteractiveBatchWithoutCapability(t *testing.T) {
	redacting := NewRedactingStore(struct{ store.ControlPlaneStore }{}, redaction.NewRegistry())
	_, err := redacting.OpenInteractiveQuestions(context.Background(), []store.OpenInteractiveQuestionCommand{{}})
	if err == nil || !strings.Contains(err.Error(), "does not support interactive Question batch operations") {
		t.Fatalf("OpenInteractiveQuestions() error=%v", err)
	}
}

var _ store.InteractiveQuestionBatchStore = (*redactingInteractiveBatchBase)(nil)
