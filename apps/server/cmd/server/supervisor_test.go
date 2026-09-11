package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
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

func TestInternalRunnerUnavailableKeepsControlPlaneRunning(t *testing.T) {
	if shouldStopControlPlaneForInternalRunner(app.ErrInternalRunnerUnavailable) {
		t.Fatal("missing internal runner binary must not stop the control plane")
	}
	if !shouldStopControlPlaneForInternalRunner(errors.New("rotate failed")) {
		t.Fatal("other internal runner failures must still stop the control plane")
	}
	if shouldStopControlPlaneForInternalRunner(nil) {
		t.Fatal("successful supervision must not stop the control plane")
	}
}

func TestRunInternalRunnerSupervisorSkipsWhenUnavailableAndStopsOnInvalidAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stopped atomic.Bool
	stop := func() { stopped.Store(true) }

	runInternalRunnerSupervisor(ctx, stop, http.NotFoundHandler())
	if stopped.Load() {
		t.Fatal("non-application handler stopped the control plane")
	}

	runInternalRunnerSupervisor(ctx, stop, &applicationHandler{})
	if stopped.Load() {
		t.Fatal("nil services stopped the control plane")
	}

	runInternalRunnerSupervisor(ctx, stop, &applicationHandler{services: &app.Services{ControlPlane: &app.Service{}}})
	if stopped.Load() {
		t.Fatal("nil runner service stopped the control plane")
	}

	t.Setenv("AGENT_BOARD_SERVER_ADDR", "not-a-host-port")
	runInternalRunnerSupervisor(ctx, stop, &applicationHandler{
		services: &app.Services{ControlPlane: &app.Service{Runners: app.NewRunnerService(nil)}},
	})
	if !stopped.Load() {
		t.Fatal("invalid listen address did not stop the control plane")
	}
}

func TestRunInternalRunnerSupervisorKeepsControlPlaneWhenBinaryMissing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stopped atomic.Bool
	stop := func() { stopped.Store(true) }

	t.Setenv("AGENT_BOARD_SERVER_ADDR", "0.0.0.0:3001")
	t.Setenv("AGENT_BOARD_RUNNER_BINARY", t.TempDir()+"/missing-agent-runner")
	t.Setenv("AGENT_BOARD_WORKSPACE_ROOT", t.TempDir())
	runInternalRunnerSupervisor(ctx, stop, &applicationHandler{
		services: &app.Services{ControlPlane: &app.Service{Runners: app.NewRunnerService(nil)}},
	})
	if stopped.Load() {
		t.Fatal("missing optional runner binary stopped the control plane")
	}

	t.Setenv("AGENT_BOARD_SERVER_ADDR", ":3001")
	runInternalRunnerSupervisor(ctx, stop, &applicationHandler{
		services: &app.Services{ControlPlane: &app.Service{Runners: app.NewRunnerService(nil)}},
	})
	if stopped.Load() {
		t.Fatal("wildcard listen address remap stopped the control plane")
	}

	t.Setenv("AGENT_BOARD_SERVER_ADDR", "[::]:3001")
	t.Setenv("PATH", "")
	t.Setenv("AGENT_BOARD_RUNNER_BINARY", "")
	runInternalRunnerSupervisor(ctx, stop, &applicationHandler{
		services: &app.Services{ControlPlane: &app.Service{Runners: app.NewRunnerService(nil)}},
	})
	if stopped.Load() {
		t.Fatal("default runner binary lookup stopped the control plane")
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
