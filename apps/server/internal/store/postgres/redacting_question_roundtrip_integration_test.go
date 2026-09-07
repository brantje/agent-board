package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRedactingStoreChoiceOptionIDRoundTrip(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "redacting-question-option-roundtrip")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "redacting-question-option-start")
	start := mustAdmit(t, s, "worker-start")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      start.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: start.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	registry := redaction.NewRegistry()
	registry.Register(f.run.ID, []string{"secret"})
	secured := evidence.NewRedactingStore(s, registry)
	options, err := json.Marshal([]store.QuestionOption{{ID: "secret-option", Label: "secret choice"}})
	if err != nil {
		t.Fatalf("marshal options: %v", err)
	}
	question, err := secured.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		RunID:     f.run.ID,
		Prompt:    "Choose the secret path",
		Kind:      "SINGLE_CHOICE",
		Options:   options,
		Blocking:  true,
		Status:    "OPEN",
	})
	if err != nil {
		t.Fatalf("create redacted choice question: %v", err)
	}
	persisted, err := secured.GetQuestion(ctx, f.project.ID, question.ID)
	if err != nil {
		t.Fatalf("get persisted question: %v", err)
	}
	var persistedOptions []store.QuestionOption
	if err := json.Unmarshal(persisted.Options, &persistedOptions); err != nil {
		t.Fatalf("decode persisted options: %v", err)
	}
	if len(persistedOptions) != 1 || persistedOptions[0].ID == "" || strings.Contains(persistedOptions[0].ID, "secret") {
		t.Fatalf("persisted options were not safely redacted: %+v", persistedOptions)
	}

	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      start.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: start.Lease.LeaseToken,
		RunStatus:  "WAITING_FOR_INPUT",
	}); err != nil {
		t.Fatalf("mark waiting: %v", err)
	}
	answered, err := secured.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID:  f.project.ID,
		QuestionID: question.ID,
		Answer: store.QuestionAnswer{
			Kind:      "SINGLE_CHOICE",
			OptionIDs: []string{persistedOptions[0].ID},
		},
		ActorType: "HUMAN",
	})
	if err != nil {
		t.Fatalf("answer with persisted redacted option id: %v", err)
	}
	if answered.Question.Status != "ANSWERED" || answered.Job == nil || answered.Job.Kind != "RESUME" {
		t.Fatalf("answer result=%+v", answered)
	}
}
