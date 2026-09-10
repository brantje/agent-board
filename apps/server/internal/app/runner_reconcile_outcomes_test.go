package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
)

func TestRunnerReconcileKeepsAmbiguousTransportFailuresUncertain(t *testing.T) {
	t.Run("registry error", func(t *testing.T) {
		storeFake := runnerOwnedSessionStore("RUNNING")
		service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{err: errors.New("reconcile probe failed")})
		if err != nil {
			t.Fatal(err)
		}

		_, err = service.Reconcile(t.Context(), "project-1", "session-1")
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != "execution_session_uncertain" {
			t.Fatalf("reconcile error=%v, want execution_session_uncertain", err)
		}
		if storeFake.session.Status != "RUNNING" {
			t.Fatalf("ambiguous registry failure changed status to %q", storeFake.session.Status)
		}
	})

	t.Run("missing transport inside reconnect window", func(t *testing.T) {
		storeFake := runnerOwnedSessionStore("RUNNING")
		registry := &runnerReconcileRegistryFake{disconnected: true, disconnectedAt: time.Now()}
		service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, registry)
		if err != nil {
			t.Fatal(err)
		}

		_, err = service.Reconcile(t.Context(), "project-1", "session-1")
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != "execution_session_uncertain" {
			t.Fatalf("reconcile error=%v, want execution_session_uncertain", err)
		}
		if storeFake.session.Status != "RUNNING" {
			t.Fatalf("missing transport inside reconnect window changed status to %q", storeFake.session.Status)
		}
	})

	t.Run("terminal transport disconnect", func(t *testing.T) {
		transport := newFakeExecutionTransport("session-1")
		transport.waitErr = runner.ErrDisconnected
		close(transport.resultCh)
		storeFake := runnerOwnedSessionStore("RUNNING")
		service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{transport: transport})
		if err != nil {
			t.Fatal(err)
		}

		_, err = service.Reconcile(t.Context(), "project-1", "session-1")
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != "execution_session_uncertain" {
			t.Fatalf("reconcile error=%v, want execution_session_uncertain", err)
		}
		if storeFake.session.Status != "RUNNING" {
			t.Fatalf("terminal transport disconnect changed status to %q", storeFake.session.Status)
		}
	})
}

func TestRunnerReconcileFailsSessionWhenTerminalDeliveryCannotComplete(t *testing.T) {
	for name, waitErr := range map[string]error{
		"delivery timeout": context.DeadlineExceeded,
		"terminal failure": errors.New("terminal result failed"),
	} {
		t.Run(name, func(t *testing.T) {
			transport := newFakeExecutionTransport("session-1")
			transport.waitErr = waitErr
			close(transport.resultCh)
			storeFake := runnerOwnedSessionStore("RUNNING")
			service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{transport: transport})
			if err != nil {
				t.Fatal(err)
			}

			_, err = service.Reconcile(t.Context(), "project-1", "session-1")
			if name == "terminal failure" && !errors.Is(err, waitErr) {
				t.Fatalf("reconcile error=%v, want original terminal failure", err)
			}
			if storeFake.session.Status != "FAILED" {
				t.Fatalf("terminal delivery failure status=%q, want FAILED", storeFake.session.Status)
			}
		})
	}
}

func TestRunnerReconcileHonorsServerContextCancellation(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	storeFake := runnerOwnedSessionStore("RUNNING")
	service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = service.Reconcile(ctx, "project-1", "session-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("reconcile error=%v, want context.Canceled", err)
	}
	if storeFake.session.Status != "RUNNING" {
		t.Fatalf("server cancellation changed durable session status to %q", storeFake.session.Status)
	}
}
