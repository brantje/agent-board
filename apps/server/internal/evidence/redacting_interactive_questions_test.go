package evidence

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type interactiveQuestionRedactionStore struct {
	store.ControlPlaneStore
	opened            store.OpenInteractiveQuestionCommand
	resolveProjectID  string
	resolveQuestionID string
}

func (s *interactiveQuestionRedactionStore) OpenInteractiveQuestion(_ context.Context, input store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionResult, error) {
	s.opened = input
	input.Question.ID = "question-1"
	return store.OpenInteractiveQuestionResult{Question: input.Question}, nil
}

func (s *interactiveQuestionRedactionStore) ResolveInteractiveQuestion(_ context.Context, projectID, questionID string) (store.ResolveInteractiveQuestionResult, error) {
	s.resolveProjectID = projectID
	s.resolveQuestionID = questionID
	return store.ResolveInteractiveQuestionResult{
		Binding: store.InteractiveQuestionBinding{QuestionID: questionID, ProjectID: projectID, State: store.InteractiveQuestionResolved},
	}, nil
}

type noInteractiveQuestionControlPlane struct{ store.ControlPlaneStore }

func TestRedactingStoreInteractiveQuestionOperations(t *testing.T) {
	const runID = "run-1"
	registry := redaction.NewRegistry()
	registry.Register(runID, []string{"secret"})
	base := &interactiveQuestionRedactionStore{}
	secured := NewRedactingStore(base, registry)

	if !secured.SupportsInteractiveQuestionStore() {
		t.Fatal("interactive Question capability was not preserved through redaction")
	}

	recommendation := "prefer secret route"
	opened, err := secured.OpenInteractiveQuestion(context.Background(), store.OpenInteractiveQuestionCommand{
		Question: store.Question{
			ProjectID:      "project-1",
			IssueID:        "issue-1",
			RunID:          runID,
			Prompt:         "do not reveal secret",
			Kind:           "SINGLE_CHOICE",
			Options:        json.RawMessage(`[{"id":"safe","label":"secret choice"}]`),
			Recommendation: &recommendation,
			Blocking:       true,
			Status:         "OPEN",
		},
		Engine:         "opencode",
		CorrelationKey: "session/request/0",
	})
	if err != nil || opened.Question.ID != "question-1" {
		t.Fatalf("OpenInteractiveQuestion()=%+v err=%v", opened, err)
	}
	if strings.Contains(base.opened.Question.Prompt, "secret") || base.opened.Question.Recommendation == nil || strings.Contains(*base.opened.Question.Recommendation, "secret") || strings.Contains(string(base.opened.Question.Options), "secret") {
		t.Fatalf("OpenInteractiveQuestion() leaked secret: %+v options=%s", base.opened.Question, base.opened.Question.Options)
	}
	if !strings.Contains(base.opened.Question.Prompt, "***") || !strings.Contains(*base.opened.Question.Recommendation, "***") || !strings.Contains(string(base.opened.Question.Options), "***") {
		t.Fatalf("OpenInteractiveQuestion() did not redact values: %+v options=%s", base.opened.Question, base.opened.Question.Options)
	}
	if base.opened.Engine != "opencode" || base.opened.CorrelationKey != "session/request/0" {
		t.Fatalf("correlation metadata changed: %+v", base.opened)
	}

	resolved, err := secured.ResolveInteractiveQuestion(context.Background(), "project-1", "question-1")
	if err != nil {
		t.Fatalf("ResolveInteractiveQuestion() error=%v", err)
	}
	if resolved.Binding.QuestionID != "question-1" || base.resolveProjectID != "project-1" || base.resolveQuestionID != "question-1" {
		t.Fatalf("ResolveInteractiveQuestion()=%+v forwarded=%q/%q", resolved, base.resolveProjectID, base.resolveQuestionID)
	}
}

func TestRedactingStoreRejectsMissingInteractiveQuestionCapability(t *testing.T) {
	secured := NewRedactingStore(&noInteractiveQuestionControlPlane{}, redaction.NewRegistry())
	if secured.SupportsInteractiveQuestionStore() {
		t.Fatal("missing interactive Question capability was reported as supported")
	}
	if _, err := secured.OpenInteractiveQuestion(context.Background(), store.OpenInteractiveQuestionCommand{}); err == nil || !strings.Contains(err.Error(), "does not support interactive Question operations") {
		t.Fatalf("OpenInteractiveQuestion() error=%v", err)
	}
	if _, err := secured.ResolveInteractiveQuestion(context.Background(), "project-1", "question-1"); err == nil || !strings.Contains(err.Error(), "does not support interactive Question operations") {
		t.Fatalf("ResolveInteractiveQuestion() error=%v", err)
	}
}
