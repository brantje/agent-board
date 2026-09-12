package runexec

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type interactiveReplyJournalStore struct {
	events []store.Event
}

func (s *interactiveReplyJournalStore) AppendEvent(_ context.Context, event store.Event) (store.Event, error) {
	sequence := int64(len(s.events) + 1)
	event.Sequence = &sequence
	s.events = append(s.events, event)
	return event, nil
}

func (s *interactiveReplyJournalStore) ListRunEvents(_ context.Context, projectID, runID string, after int64, limit int) ([]store.Event, error) {
	result := make([]store.Event, 0)
	for _, event := range s.events {
		if event.ProjectID != projectID || event.RunID == nil || *event.RunID != runID || event.Sequence == nil || *event.Sequence <= after {
			continue
		}
		result = append(result, event)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func TestInteractiveQuestionReplyJournalTracksOnlyUnresolvedAcceptedBindings(t *testing.T) {
	journal := &interactiveReplyJournalStore{}
	recorder, err := evidence.NewRecorder(journal, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &interactiveLifecycleStore{}
	q := &interactiveQuestioner{
		interactive: lifecycle,
		events:      recorder,
		eventReader: journal,
		safe:        interactiveSafeContext(),
		engine:      "opencode",
	}
	accepted := []engine.AcceptedInteractiveQuestionReply{
		{QuestionID: "question-1", CorrelationKey: "ses_1/que_1/0"},
		{QuestionID: "question-2", CorrelationKey: "ses_1/que_1/1"},
	}
	if err := q.MarkReplyAccepted(context.Background(), accepted); err != nil {
		t.Fatalf("MarkReplyAccepted() error=%v", err)
	}
	pending, err := q.ListReplyAccepted(context.Background())
	if err != nil {
		t.Fatalf("ListReplyAccepted() error=%v", err)
	}
	if len(pending) != 2 || pending[0] != accepted[0] || pending[1] != accepted[1] {
		t.Fatalf("pending=%+v want %+v", pending, accepted)
	}

	if err := q.Resolve(context.Background(), "question-1"); err != nil {
		t.Fatalf("Resolve() error=%v", err)
	}
	pending, err = q.ListReplyAccepted(context.Background())
	if err != nil {
		t.Fatalf("ListReplyAccepted() after resolve error=%v", err)
	}
	if len(pending) != 1 || pending[0] != accepted[1] {
		t.Fatalf("pending after resolve=%+v", pending)
	}
	if len(journal.events) != 2 || journal.events[0].Type != interactiveQuestionReplyAcceptedEvent || journal.events[1].Type != interactiveQuestionResolvedEvent {
		t.Fatalf("journal events=%+v", journal.events)
	}
}

func TestInteractiveQuestionReplyJournalRejectsInvalidTrackingState(t *testing.T) {
	q := &interactiveQuestioner{}
	if err := q.MarkReplyAccepted(context.Background(), []engine.AcceptedInteractiveQuestionReply{{QuestionID: "question-1", CorrelationKey: "key"}}); err == nil {
		t.Fatal("MarkReplyAccepted without journal capability unexpectedly succeeded")
	}
	if _, err := q.ListReplyAccepted(context.Background()); err == nil {
		t.Fatal("ListReplyAccepted without journal capability unexpectedly succeeded")
	}

	journal := &interactiveReplyJournalStore{}
	recorder, err := evidence.NewRecorder(journal, nil)
	if err != nil {
		t.Fatal(err)
	}
	q = &interactiveQuestioner{events: recorder, eventReader: journal, safe: interactiveSafeContext(), engine: "opencode"}
	for _, accepted := range [][]engine.AcceptedInteractiveQuestionReply{
		nil,
		{{QuestionID: "", CorrelationKey: "key"}},
		{{QuestionID: "question-1", CorrelationKey: ""}},
		{{QuestionID: "question-1", CorrelationKey: "key"}, {QuestionID: "question-1", CorrelationKey: "other"}},
	} {
		if err := q.MarkReplyAccepted(context.Background(), accepted); err == nil {
			t.Fatalf("invalid accepted bindings %+v unexpectedly succeeded", accepted)
		}
	}
}
