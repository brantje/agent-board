package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

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

func TestCoordinatorDoesNotTreatShutdownCancelledRenewalAsLeaseLoss(t *testing.T) {
	claim := fakeAdmission("shutdown-renewal")
	base := &fakeSchedulerStore{}
	fs := &blockingRenewalStore{
		fakeSchedulerStore: base,
		renewalStarted:    make(chan struct{}),
	}
	parent, cancel := context.WithCancel(context.Background())
	processor := processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		select {
		case <-fs.renewalStarted:
		case <-time.After(time.Second):
			t.Fatal("lease renewal did not start")
		}
		cancel()
		return Result{RunStatus: "COMPLETED"}, nil
	})
	cfg := testConfig()
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.HeartbeatInterval = 5 * time.Millisecond
	reported := make(chan error, 1)
	cfg.ReportError = func(err error) { reported <- err }
	c, err := New(fs, processor, testReconciler(), cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	c.process(parent, claim)

	transitions := base.transitionsSnapshot()
	if len(transitions) != 1 || transitions[0].RunStatus != "COMPLETED" {
		t.Fatalf("transitions=%+v want completed final transition", transitions)
	}
	select {
	case err := <-reported:
		t.Fatalf("shutdown-cancelled renewal reported as lease loss: %v", err)
	default:
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

type blockingRenewalStore struct {
	*fakeSchedulerStore
	renewalStarted chan struct{}
	startOnce      sync.Once
}

func (s *blockingRenewalStore) RenewLease(ctx context.Context, _ string, _ string, _ string, _ time.Duration) (store.SchedulerLease, error) {
	s.startOnce.Do(func() { close(s.renewalStarted) })
	<-ctx.Done()
	return store.SchedulerLease{}, ctx.Err()
}
