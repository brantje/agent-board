package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCoordinatorCancelRunHoldsActiveMutexUntilCancelReturns(t *testing.T) {
	claim := fakeAdmission("cancel-mutex")
	c, err := New(&fakeSchedulerStore{}, noopProcessor(), testReconciler(), testConfig())
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	unblock := make(chan struct{})
	firstCtx, firstCancel := context.WithCancelCause(context.Background())
	defer firstCancel(nil)
	blockingCancel := func(err error) {
		firstCancel(err)
		<-unblock
	}

	token := c.registerActiveRun(claim, blockingCancel)
	defer c.unregisterActiveRun(claim, token)

	done := make(chan bool, 1)
	go func() {
		done <- c.CancelRun(claim.Job.ProjectID, claim.Run.ID)
	}()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if errors.Is(context.Cause(firstCtx), ErrRunCancellation) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !errors.Is(context.Cause(firstCtx), ErrRunCancellation) {
		t.Fatalf("first registration was not cancelled: %v", context.Cause(firstCtx))
	}

	_, secondCancel := context.WithCancelCause(context.Background())
	defer secondCancel(nil)
	registered := make(chan uint64, 1)
	go func() {
		registered <- c.registerActiveRun(claim, secondCancel)
	}()

	select {
	case <-registered:
		t.Fatal("new registration succeeded while CancelRun still held activeMu during blocking cancel")
	case <-time.After(50 * time.Millisecond):
	}

	close(unblock)
	if !<-done {
		t.Fatal("CancelRun returned false")
	}

	select {
	case secondToken := <-registered:
		c.unregisterActiveRun(claim, secondToken)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("new registration did not proceed after CancelRun released activeMu")
	}
}

func TestCoordinatorStaleUnregisterPreservesNewRegistration(t *testing.T) {
	claim := fakeAdmission("registration-race")
	c, err := New(&fakeSchedulerStore{}, noopProcessor(), testReconciler(), testConfig())
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	firstCtx, firstCancel := context.WithCancelCause(context.Background())
	defer firstCancel(nil)
	secondCtx, secondCancel := context.WithCancelCause(context.Background())
	defer secondCancel(nil)

	firstToken := c.registerActiveRun(claim, firstCancel)
	secondToken := c.registerActiveRun(claim, secondCancel)
	c.unregisterActiveRun(claim, firstToken)

	if !c.CancelRun(claim.Job.ProjectID, claim.Run.ID) {
		t.Fatal("newer active registration was removed by stale unregister")
	}
	if !errors.Is(context.Cause(secondCtx), ErrRunCancellation) {
		t.Fatalf("newer registration cause=%v", context.Cause(secondCtx))
	}
	if context.Cause(firstCtx) != nil {
		t.Fatalf("stale registration was cancelled: %v", context.Cause(firstCtx))
	}

	c.unregisterActiveRun(claim, secondToken)
	if c.CancelRun(claim.Job.ProjectID, claim.Run.ID) {
		t.Fatal("cancel succeeded after active registration was removed")
	}
}

func TestCoordinatorFinalResultWinsOverRacingCancellation(t *testing.T) {
	claim := fakeAdmission("final-result-race")
	fs := &fakeSchedulerStore{}
	var coordinator *Coordinator
	processor := processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		if !coordinator.CancelRun(claim.Job.ProjectID, claim.Run.ID) {
			t.Fatal("expected active Run cancellation to be accepted")
		}
		return Result{RunStatus: "READY_FOR_REVIEW"}, nil
	})

	var err error
	coordinator, err = New(fs, processor, testReconciler(), testConfig())
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	coordinator.process(context.Background(), claim)

	transitions := fs.transitionsSnapshot()
	if len(transitions) != 1 {
		t.Fatalf("transitions=%+v", transitions)
	}
	if transitions[0].RunStatus != "READY_FOR_REVIEW" {
		t.Fatalf("final status=%s want READY_FOR_REVIEW", transitions[0].RunStatus)
	}
}
