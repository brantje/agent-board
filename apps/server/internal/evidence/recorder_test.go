package evidence

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type memoryEventStore struct {
	persisted   bool
	appendCount int
}

func (s *memoryEventStore) AppendEvent(_ context.Context, event store.Event) (store.Event, error) {
	s.persisted = true
	s.appendCount++
	event.ID = "event-1"
	sequence := int64(1)
	event.Sequence = &sequence
	return event, nil
}

type observingPublisher struct {
	store *memoryEventStore
	seen  store.Event
}

func (p *observingPublisher) Publish(_ context.Context, event store.Event) error {
	if !p.store.persisted {
		return errors.New("published before persistence")
	}
	p.seen = event
	return nil
}

func TestRecorderPersistsBeforePublishing(t *testing.T) {
	store := &memoryEventStore{}
	publisher := &observingPublisher{store: store}
	recorder, err := NewRecorder(store, publisher)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := recorder.Record(context.Background(), storeEvent("run.started"))
	if err != nil {
		t.Fatal(err)
	}
	if publisher.seen.ID != persisted.ID || publisher.seen.Sequence == nil || *publisher.seen.Sequence != 1 {
		t.Fatalf("publisher did not receive durable event: %+v", publisher.seen)
	}
}

type failingPublisher struct{ calls int }

func (p *failingPublisher) Publish(context.Context, store.Event) error {
	p.calls++
	return errors.New("live publication unavailable")
}

func TestRecorderDoesNotMakeCommittedEventRetryableAfterPublishFailure(t *testing.T) {
	store := &memoryEventStore{}
	publisher := &failingPublisher{}
	var reported error
	recorder, err := NewRecorder(store, publisher, func(err error) { reported = err })
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := recorder.Record(context.Background(), storeEvent("run.started"))
	if err != nil {
		t.Fatalf("Record returned a retryable error after persistence: %v", err)
	}
	if persisted.ID == "" || store.appendCount != 1 {
		t.Fatalf("persisted=%+v appendCount=%d, want exactly one durable event", persisted, store.appendCount)
	}
	if publisher.calls != 1 || reported == nil {
		t.Fatalf("publisher calls=%d reported=%v", publisher.calls, reported)
	}
}

type failingEventStore struct{}

func (failingEventStore) AppendEvent(context.Context, store.Event) (store.Event, error) {
	return store.Event{}, errors.New("append rejected")
}

func TestRecorderReturnsAppendFailureWithoutPublishing(t *testing.T) {
	publisher := &failingPublisher{}
	recorder, err := NewRecorder(failingEventStore{}, publisher)
	if err != nil {
		t.Fatal(err)
	}
	_, err = recorder.Record(context.Background(), storeEvent("run.started"))
	if err == nil {
		t.Fatal("expected append failure")
	}
	if publisher.calls != 0 {
		t.Fatalf("publish called %d times after append failure", publisher.calls)
	}
}

func TestPublishPersistedIsNoOpWithoutPublisher(t *testing.T) {
	var nilRecorder *Recorder
	nilRecorder.PublishPersisted(context.Background(), storeEvent("run.started"))

	recorder, err := NewRecorder(&memoryEventStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder.PublishPersisted(context.Background(), storeEvent("run.started"))
}

func TestPublishPersistedNotifiesAfterExternalCommit(t *testing.T) {
	hub := NewHub()
	recorder, err := NewRecorder(&memoryEventStore{}, hub)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := hub.Subscribe(ctx, "run-1")
	defer unsubscribe()

	runID := "run-1"
	sequence := int64(9)
	committed := store.Event{ID: "tx-event", Type: "question.answered", ProjectID: "project", RunID: &runID, Sequence: &sequence}
	recorder.PublishPersisted(context.Background(), committed)
	got := receiveEvent(t, events)
	if got.ID != committed.ID || got.Sequence == nil || *got.Sequence != sequence {
		t.Fatalf("live fan-out=%+v want %+v", got, committed)
	}
}

func TestEncodePayloadRejectsUnmarshalableValues(t *testing.T) {
	_, err := EncodePayload(make(chan int))
	if err == nil {
		t.Fatal("expected encode failure for channel payload")
	}
}

func storeEvent(kind string) store.Event {
	return store.Event{Type: kind, ProjectID: "project"}
}
