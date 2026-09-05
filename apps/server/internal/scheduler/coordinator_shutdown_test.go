package scheduler

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCoordinatorPersistsFinalResultDuringShutdown(t *testing.T) {
	claim := fakeAdmission("shutdown-final")
	base := &fakeSchedulerStore{}
	fs := &transitionContextStore{fakeSchedulerStore: base}
	parent, cancel := context.WithCancel(context.Background())
	processor := processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		cancel()
		return Result{RunStatus: "COMPLETED"}, nil
	})
	c, err := New(fs, processor, testReconciler(), testConfig())
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	c.process(parent, claim)

	transitions := base.transitionsSnapshot()
	if len(transitions) != 1 || transitions[0].RunStatus != "COMPLETED" {
		t.Fatalf("transitions=%+v want completed final transition", transitions)
	}
	if fs.transitionContextErr != nil {
		t.Fatalf("transition context err=%v want detached persistence context", fs.transitionContextErr)
	}
}

type transitionContextStore struct {
	*fakeSchedulerStore
	transitionContextErr error
}

func (s *transitionContextStore) TransitionAdmittedJob(ctx context.Context, input store.SchedulerTransition) (store.Run, error) {
	s.transitionContextErr = ctx.Err()
	return s.fakeSchedulerStore.TransitionAdmittedJob(ctx, input)
}
