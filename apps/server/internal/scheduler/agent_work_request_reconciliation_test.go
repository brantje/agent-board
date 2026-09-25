package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCoordinatorReconcilesPendingAgentWorkThroughOptionalStoreCapability(t *testing.T) {
	processor := processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		return Result{RunStatus: "COMPLETED"}, nil
	})

	t.Run("unsupported", func(t *testing.T) {
		coordinator, err := New(&fakeSchedulerStore{}, processor, testReconciler(), testConfig())
		if err != nil {
			t.Fatal(err)
		}
		handled, err := coordinator.reconcilePendingAgentWorkRequest(t.Context())
		if err != nil || handled {
			t.Fatalf("handled=%v err=%v", handled, err)
		}
	})

	t.Run("error", func(t *testing.T) {
		want := errors.New("reconcile comment work")
		work := &agentWorkReconciliationStore{fakeSchedulerStore: &fakeSchedulerStore{}, err: want}
		coordinator, err := New(work, processor, testReconciler(), testConfig())
		if err != nil {
			t.Fatal(err)
		}
		handled, err := coordinator.reconcilePendingAgentWorkRequest(t.Context())
		if handled || !errors.Is(err, want) {
			t.Fatalf("handled=%v err=%v", handled, err)
		}
	})

	t.Run("publishes persisted events", func(t *testing.T) {
		event := store.Event{ID: "comment-work-run-created", Type: "run.created", ProjectID: "project"}
		work := &agentWorkReconciliationStore{
			fakeSchedulerStore: &fakeSchedulerStore{},
			result: store.AgentWorkRequestReconciliationResult{Handled: true, Events: []store.Event{event}},
		}
		publisher := &agentWorkEventPublisher{}
		cfg := testConfig()
		cfg.PersistedEvents = publisher
		coordinator, err := New(work, processor, testReconciler(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		handled, err := coordinator.reconcilePendingAgentWorkRequest(t.Context())
		if err != nil || !handled {
			t.Fatalf("handled=%v err=%v", handled, err)
		}
		if !work.called || len(publisher.events) != 1 || publisher.events[0].ID != event.ID {
			t.Fatalf("called=%v events=%+v", work.called, publisher.events)
		}
	})
}

type agentWorkReconciliationStore struct {
	*fakeSchedulerStore
	result store.AgentWorkRequestReconciliationResult
	err    error
	called bool
}

func (s *agentWorkReconciliationStore) ReconcilePendingAgentWorkRequest(context.Context) (store.AgentWorkRequestReconciliationResult, error) {
	s.called = true
	return s.result, s.err
}

type agentWorkEventPublisher struct {
	events []store.Event
}

func (p *agentWorkEventPublisher) PublishPersisted(_ context.Context, event store.Event) {
	p.events = append(p.events, event)
}
