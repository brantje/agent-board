package evidence

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type memoryEventStore struct {
	persisted bool
}

func (s *memoryEventStore) AppendEvent(_ context.Context, event store.Event) (store.Event, error) {
	s.persisted = true
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

func storeEvent(kind string) store.Event {
	return store.Event{Type: kind, ProjectID: "project"}
}
