package main

import (
	"context"
	"errors"
	"testing"
)

func TestSuperviseStopsServerWhenSchedulerFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	schedulerDone := make(chan error, 1)
	schedulerDone <- errors.New("scheduler failed")
	close(schedulerDone)

	code := supervise(ctx, cancel, func() int {
		<-ctx.Done()
		return 0
	}, schedulerDone)
	if code != 1 {
		t.Fatalf("exit code=%d, want 1", code)
	}
	if ctx.Err() == nil {
		t.Fatal("scheduler failure did not cancel server context")
	}
}

func TestSuperviseTreatsSchedulerExitAfterCancellationAsExpected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	schedulerDone := make(chan error, 1)
	schedulerDone <- context.Canceled
	close(schedulerDone)

	code := supervise(ctx, cancel, func() int {
		<-ctx.Done()
		return 0
	}, schedulerDone)
	if code != 0 {
		t.Fatalf("exit code=%d, want 0", code)
	}
}

func TestSuperviseTreatsNilSchedulerExitAsFailureWhileServing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	schedulerDone := make(chan error, 1)
	schedulerDone <- nil
	close(schedulerDone)

	code := supervise(ctx, cancel, func() int {
		<-ctx.Done()
		return 0
	}, schedulerDone)
	if code != 1 {
		t.Fatalf("exit code=%d, want 1", code)
	}
}
