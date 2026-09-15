package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCoordinatorWakeInterruptsPollWait(t *testing.T) {
	config := testConfig()
	config.PollInterval = time.Hour
	coordinator, err := New(
		&fakeSchedulerStore{},
		processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
			return Result{RunStatus: "COMPLETED"}, nil
		}),
		testReconciler(),
		config,
	)
	if err != nil {
		t.Fatalf("New() error=%v", err)
	}

	done := make(chan bool, 1)
	go func() { done <- coordinator.waitForPoll(context.Background()) }()
	coordinator.Wake()

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("wake must resume the coordinator")
		}
	case <-time.After(time.Second):
		t.Fatal("wake did not interrupt poll wait")
	}
}

func TestCoordinatorWakeCoalesces(t *testing.T) {
	config := testConfig()
	config.PollInterval = time.Hour
	coordinator, err := New(
		&fakeSchedulerStore{},
		processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
			return Result{RunStatus: "COMPLETED"}, nil
		}),
		testReconciler(),
		config,
	)
	if err != nil {
		t.Fatalf("New() error=%v", err)
	}

	coordinator.Wake()
	coordinator.Wake()
	if !coordinator.waitForPoll(context.Background()) {
		t.Fatal("queued wake must resume the coordinator")
	}
	select {
	case <-coordinator.wake:
		t.Fatal("wake signals must coalesce")
	default:
	}
}
