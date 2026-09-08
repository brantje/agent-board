package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestInteractiveQuestionAnswerPersistsAuditEventsInOrder(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "interactive-question-events")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "interactive-question-events-start")
	admission := mustAdmit(t, s, "interactive-question-events-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID,
		JobID: admission.Job.ID,
		RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	opened, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
		Question: store.Question{
			ProjectID: f.project.ID,
			IssueID: f.issue.ID,
			RunID: f.run.ID,
			Prompt: "Choose the native path",
			Kind: "SINGLE_CHOICE",
			Options: json.RawMessage(`[{"id":"option-0","label":"A"},{"id":"option-1","label":"B"}]`),
			Blocking: true,
			Status: "OPEN",
		},
		Engine: "opencode",
		CorrelationKey: "session-events/request-events/0",
	})
	if err != nil {
		t.Fatalf("open interactive question: %v", err)
	}

	answered, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID: f.project.ID,
		QuestionID: opened.Question.ID,
		Answer: store.QuestionAnswer{Kind: "SINGLE_CHOICE", OptionIDs: []string{"option-1"}},
		ActorType: "HUMAN",
	})
	if err != nil {
		t.Fatalf("answer interactive question: %v", err)
	}
	if answered.Run.Status != "WAITING_FOR_INPUT" || answered.Job != nil {
		t.Fatalf("answer unexpectedly resumed live run: %+v", answered)
	}

	events, err := s.ListRunEvents(ctx, f.project.ID, f.run.ID, 0, 500)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	answeredIndex := eventIndex(events, "question.answered")
	decisionIndex := eventIndex(events, "decision.recorded")
	if answeredIndex < 0 || decisionIndex < 0 || answeredIndex >= decisionIndex {
		t.Fatalf("event order=%v", eventTypeList(events))
	}
	if events[answeredIndex].Sequence == nil || events[decisionIndex].Sequence == nil || *events[answeredIndex].Sequence >= *events[decisionIndex].Sequence {
		t.Fatalf("answer sequence=%v decision sequence=%v", events[answeredIndex].Sequence, events[decisionIndex].Sequence)
	}

	var payload struct {
		QuestionID string `json:"questionId"`
		Answer store.QuestionAnswer `json:"answer"`
	}
	if err := json.Unmarshal(events[answeredIndex].Payload, &payload); err != nil {
		t.Fatalf("decode answer event: %v", err)
	}
	if payload.QuestionID != opened.Question.ID || len(payload.Answer.OptionIDs) != 1 || payload.Answer.OptionIDs[0] != "option-1" {
		t.Fatalf("answer payload=%+v", payload)
	}
}

func eventIndex(events []store.Event, eventType string) int {
	for index, event := range events {
		if event.Type == eventType {
			return index
		}
	}
	return -1
}

func eventTypeList(events []store.Event) []string {
	values := make([]string, 0, len(events))
	for _, event := range events {
		values = append(values, event.Type)
	}
	return values
}

func TestInteractiveResolutionCommitsLifecycleEvidenceAtomically(t *testing.T) {
	for _, rejectedType := range []string{"engine.question_binding_resolved", "run.resumed"} {
		t.Run(rejectedType, func(t *testing.T) {
			s := New(testPool(t))
			ctx := context.Background()
			f := seedRunFixture(t, s, "atomic-interactive-resolution")
			enqueueFixtureRun(t, s, f, f.run, "atomic-interactive-resolution-start")
			admission := mustAdmit(t, s, "atomic-interactive-resolution-worker")
			if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
				ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
				LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
			}); err != nil {
				t.Fatal(err)
			}
			opened, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
				Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "Continue?", Kind: "TEXT", Blocking: true},
				Engine:   "opencode", CorrelationKey: "atomic-resolution/0",
			})
			if err != nil {
				t.Fatal(err)
			}
			answer := "continue"
			if _, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
				ProjectID: f.project.ID, QuestionID: opened.Question.ID,
				Answer: store.QuestionAnswer{Kind: "TEXT", Text: &answer}, ActorType: "HUMAN",
			}); err != nil {
				t.Fatal(err)
			}
			// Reject an actual event INSERT, after the binding and Run updates.
			if _, err := s.pool.Exec(ctx, `ALTER TABLE events ADD CONSTRAINT reject_resolution_event CHECK (type <> '`+rejectedType+`')`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ResolveInteractiveQuestion(ctx, f.project.ID, opened.Question.ID); err == nil {
				t.Fatal("resolution committed without required lifecycle evidence")
			}
			run, err := s.GetRun(ctx, f.project.ID, f.run.ID)
			if err != nil || run.Status != "WAITING_FOR_INPUT" {
				t.Fatalf("failed evidence write changed Run: %+v err=%v", run, err)
			}
			var bindingState string
			if err := s.pool.QueryRow(ctx, `SELECT state FROM engine_question_bindings WHERE question_id=$1`, opened.Question.ID).Scan(&bindingState); err != nil {
				t.Fatal(err)
			}
			if bindingState != "ANSWERED" {
				t.Fatalf("failed evidence write changed binding: %s", bindingState)
			}
			if _, err := s.pool.Exec(ctx, `ALTER TABLE events DROP CONSTRAINT reject_resolution_event`); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := s.ResolveInteractiveQuestion(ctx, f.project.ID, opened.Question.ID); err != nil {
					t.Fatalf("retry %d: %v", attempt, err)
				}
			}
			events, err := s.ListRunEvents(ctx, f.project.ID, f.run.ID, 0, 500)
			if err != nil {
				t.Fatal(err)
			}
			for _, typ := range []string{"engine.question_binding_resolved", "run.resumed"} {
				count := 0
				for _, event := range events {
					if event.Type == typ {
						count++
					}
				}
				if count != 1 {
					t.Fatalf("%s events=%d want 1", typ, count)
				}
			}
			if eventIndex(events, "question.answered") >= eventIndex(events, "run.resumed") {
				t.Fatalf("event order=%v", eventTypeList(events))
			}
		})
	}
}
