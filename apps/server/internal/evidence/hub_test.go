package evidence

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestHubPublishesPersistedEventToSubscriber(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, unsubscribe := hub.Subscribe(ctx, "run-1")
	defer unsubscribe()

	sequence := int64(3)
	runID := "run-1"
	want := store.Event{ID: "event-1", Type: "run.started", RunID: &runID, Sequence: &sequence}
	if err := hub.Publish(context.Background(), want); err != nil {
		t.Fatal(err)
	}

	got := receiveEvent(t, events)
	if got.ID != want.ID || got.Type != want.Type || got.Sequence == nil || *got.Sequence != sequence {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
}

func TestHubUnsubscribeOnContextCancel(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	events, _ := hub.Subscribe(ctx, "run-1")

	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		empty := len(hub.byRunID["run-1"]) == 0
		hub.mu.Unlock()
		if empty {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	runID := "run-1"
	if err := hub.Publish(context.Background(), store.Event{ID: "late", Type: "run.started", RunID: &runID}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		t.Fatalf("cancelled subscriber received %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubSlowSubscriberDoesNotBlockPublish(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, unsubscribe := hub.Subscribe(ctx, "run-1")
	defer unsubscribe()

	runID := "run-1"
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < hubSubscriberBuffer+8; i++ {
			sequence := int64(i + 1)
			if err := hub.Publish(context.Background(), store.Event{
				ID:       "event",
				Type:     "agent.message",
				RunID:    &runID,
				Sequence: &sequence,
			}); err != nil {
				t.Errorf("Publish() error=%v", err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}

func TestHubPublishDoesNotFailDurableRecord(t *testing.T) {
	storeEvents := &memoryEventStore{}
	hub := NewHub()
	recorder, err := NewRecorder(storeEvents, hub)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := hub.Subscribe(ctx, "run-1")
	defer unsubscribe()

	runID := "run-1"
	persisted, err := recorder.Record(context.Background(), store.Event{Type: "run.started", ProjectID: "project", RunID: &runID})
	if err != nil {
		t.Fatal(err)
	}
	got := receiveEvent(t, events)
	if got.ID != persisted.ID {
		t.Fatalf("live event=%+v persisted=%+v", got, persisted)
	}
}

func TestHubIgnoresEventsWithoutRun(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := hub.Subscribe(ctx, "run-1")
	defer unsubscribe()

	if err := hub.Publish(context.Background(), store.Event{ID: "project-event", Type: "issue.created"}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		t.Fatalf("run subscriber received project event %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestHubIsolatesSubscribersByRun(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, unsubFirst := hub.Subscribe(ctx, "run-1")
	defer unsubFirst()
	second, unsubSecond := hub.Subscribe(ctx, "run-2")
	defer unsubSecond()

	runID := "run-1"
	if err := hub.Publish(context.Background(), store.Event{ID: "only-run-1", Type: "run.started", RunID: &runID}); err != nil {
		t.Fatal(err)
	}
	got := receiveEvent(t, first)
	if got.ID != "only-run-1" {
		t.Fatalf("run-1 event=%+v", got)
	}
	select {
	case event := <-second:
		t.Fatalf("run-2 subscriber received %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestHubConcurrentPublishAndUnsubscribe(t *testing.T) {
	hub := NewHub()
	runID := "run-1"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			_, unsubscribe := hub.Subscribe(ctx, runID)
			for n := 0; n < 16; n++ {
				_ = hub.Publish(context.Background(), store.Event{ID: "e", Type: "agent.message", RunID: &runID})
			}
			cancel()
			unsubscribe()
		}()
	}
	wg.Wait()
}

func TestHubSubscribeNilHubOrEmptyRunIDIsIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var nilHub *Hub
	idle, unsubscribe := nilHub.Subscribe(ctx, "run-1")
	unsubscribe()
	select {
	case event := <-idle:
		t.Fatalf("nil hub delivered %+v", event)
	default:
	}

	hub := NewHub()
	idle, unsubscribe = hub.Subscribe(ctx, "")
	defer unsubscribe()
	runID := "run-1"
	if err := hub.Publish(context.Background(), store.Event{ID: "ignored", Type: "run.started", RunID: &runID}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-idle:
		t.Fatalf("empty runID subscriber delivered %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestHubRemoveIsSafeWhenAlreadyDetached(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, unsubscribe := hub.Subscribe(ctx, "run-1")
	unsubscribe()

	hub.remove(nil)
	hub.remove(&hubSubscriber{id: 1, runID: "run-1"})
	hub.remove(&hubSubscriber{id: 2, runID: "missing-run"})

	var nilHub *Hub
	nilHub.remove(&hubSubscriber{id: 1, runID: "run-1"})
}

func TestHubDropOldestKeepsNewestEvents(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := hub.Subscribe(ctx, "run-1")
	defer unsubscribe()

	runID := "run-1"
	total := hubSubscriberBuffer + 5
	for i := 1; i <= total; i++ {
		sequence := int64(i)
		if err := hub.Publish(context.Background(), store.Event{
			ID:       fmt.Sprintf("event-%d", i),
			Type:     "agent.message",
			RunID:    &runID,
			Sequence: &sequence,
		}); err != nil {
			t.Fatal(err)
		}
	}

	got := make([]int64, 0, hubSubscriberBuffer)
	deadline := time.Now().Add(time.Second)
	for len(got) < hubSubscriberBuffer {
		if time.Now().After(deadline) {
			t.Fatalf("drained %d events, want %d", len(got), hubSubscriberBuffer)
		}
		select {
		case event := <-events:
			if event.Sequence == nil {
				t.Fatalf("missing sequence on %+v", event)
			}
			got = append(got, *event.Sequence)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	select {
	case event := <-events:
		t.Fatalf("buffer retained extra event %+v", event)
	default:
	}
	if got[0] != int64(total-hubSubscriberBuffer+1) || got[len(got)-1] != int64(total) {
		t.Fatalf("retained sequences=%v want newest %d events ending at %d", got, hubSubscriberBuffer, total)
	}
}

func receiveEvent(t *testing.T, events <-chan store.Event) store.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hub event")
		return store.Event{}
	}
}
