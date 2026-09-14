package scheduler

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerPublishesPersistedTransitionEventsAfterMutationReturns(t *testing.T) {
	event := store.Event{ID: "event-transition", Type: "issue.status_changed", ProjectID: "project"}
	mutations := &persistedEventMutationStore{
		fakeSchedulerStore: &fakeSchedulerStore{},
		transitionResult: store.SchedulerMutationResult{
			Run:    store.Run{ID: "run", ProjectID: "project", Status: "FAILED"},
			Events: []store.Event{event},
		},
	}
	publisher := &orderingPersistedEventPublisher{mutations: mutations}
	coordinator := persistedEventCoordinator(t, mutations, publisher)

	if _, err := coordinator.transitionAdmittedJob(context.Background(), store.SchedulerTransition{
		ProjectID: "project", JobID: "job", RunID: "run", LeaseToken: "lease", RunStatus: "FAILED",
	}); err != nil {
		t.Fatalf("transitionAdmittedJob() error=%v", err)
	}
	if len(publisher.events) != 1 || publisher.events[0].ID != event.ID {
		t.Fatalf("published events=%+v", publisher.events)
	}
	if !publisher.afterMutation {
		t.Fatal("persisted Event was published before the mutation returned")
	}
}

func TestSchedulerDoesNotPublishWhenTransitionPersistsNoEvent(t *testing.T) {
	mutations := &persistedEventMutationStore{
		fakeSchedulerStore: &fakeSchedulerStore{},
		transitionResult: store.SchedulerMutationResult{
			Run: store.Run{ID: "run", ProjectID: "project", Status: "FAILED"},
		},
	}
	publisher := &orderingPersistedEventPublisher{mutations: mutations}
	coordinator := persistedEventCoordinator(t, mutations, publisher)

	if _, err := coordinator.transitionAdmittedJob(context.Background(), store.SchedulerTransition{
		ProjectID: "project", JobID: "job", RunID: "run", LeaseToken: "lease", RunStatus: "FAILED",
	}); err != nil {
		t.Fatalf("transitionAdmittedJob() error=%v", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("published events=%+v want none", publisher.events)
	}
}

func TestSchedulerPublishesPersistedReconciliationEventsAfterMutationReturns(t *testing.T) {
	event := store.Event{ID: "event-reconciliation", Type: "issue.status_changed", ProjectID: "project"}
	mutations := &persistedEventMutationStore{
		fakeSchedulerStore: &fakeSchedulerStore{},
		reconciliationResult: store.SchedulerMutationResult{
			Run:    store.Run{ID: "run", ProjectID: "project", Status: "FAILED"},
			Events: []store.Event{event},
		},
	}
	publisher := &orderingPersistedEventPublisher{mutations: mutations}
	coordinator := persistedEventCoordinator(t, mutations, publisher)

	if _, err := coordinator.resolveReconciliation(context.Background(), store.SchedulerReconciliation{
		ProjectID: "project", JobID: "job", RunID: "run", LeaseToken: "lease", Outcome: store.SchedulerReconciliationFailed,
	}); err != nil {
		t.Fatalf("resolveReconciliation() error=%v", err)
	}
	if len(publisher.events) != 1 || publisher.events[0].ID != event.ID {
		t.Fatalf("published events=%+v", publisher.events)
	}
	if !publisher.afterMutation {
		t.Fatal("reconciliation Event was published before the mutation returned")
	}
}

func persistedEventCoordinator(t *testing.T, mutations *persistedEventMutationStore, publisher PersistedEventPublisher) *Coordinator {
	t.Helper()
	cfg := testConfig()
	cfg.PersistedEvents = publisher
	coordinator, err := New(mutations, processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		return Result{RunStatus: "COMPLETED"}, nil
	}), testReconciler(), cfg)
	if err != nil {
		t.Fatalf("New() error=%v", err)
	}
	return coordinator
}

type persistedEventMutationStore struct {
	*fakeSchedulerStore
	transitionResult     store.SchedulerMutationResult
	reconciliationResult store.SchedulerMutationResult
	mutationReturned     bool
}

func (s *persistedEventMutationStore) TransitionAdmittedJobMutation(context.Context, store.SchedulerTransition) (store.SchedulerMutationResult, error) {
	s.mutationReturned = true
	return s.transitionResult, nil
}

func (s *persistedEventMutationStore) ResolveReconciliationMutation(context.Context, store.SchedulerReconciliation) (store.SchedulerMutationResult, error) {
	s.mutationReturned = true
	return s.reconciliationResult, nil
}

type orderingPersistedEventPublisher struct {
	mutations     *persistedEventMutationStore
	events        []store.Event
	afterMutation bool
}

func (p *orderingPersistedEventPublisher) PublishPersisted(_ context.Context, event store.Event) {
	p.afterMutation = p.mutations != nil && p.mutations.mutationReturned
	p.events = append(p.events, event)
}
