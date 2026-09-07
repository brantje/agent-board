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
