package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const retainedTerminalDeliveryGrace = 2 * time.Second

type ExecutionSessionReconcileStore interface {
	ExecutionSessionStore
	ListProjects(context.Context) ([]store.Project, error)
	ListExecutionSessions(context.Context, string, []string) ([]store.ExecutionSession, error)
}

func (s *ExecutionSessionService) ReconcileAll(ctx context.Context) error {
	return s.ReconcileAllWithReporter(ctx, nil)
}

func (s *ExecutionSessionService) ReconcileAllWithReporter(ctx context.Context, report func(error)) error {
	reconcileStore, ok := s.store.(ExecutionSessionReconcileStore)
	if !ok {
		return fmt.Errorf("execution session store does not support reconciliation")
	}
	projects, err := reconcileStore.ListProjects(ctx)
	if err != nil {
		return translateStoreError(err, "project")
	}
	var errs []error
	for _, project := range projects {
		sessions, err := reconcileStore.ListExecutionSessions(ctx, project.ID, []string{"PENDING", "STARTING", "RUNNING"})
		if err != nil {
			errs = append(errs, fmt.Errorf("list Execution Sessions for project %s: %w", project.ID, err))
			continue
		}
		for _, session := range sessions {
			process, err := s.Reconcile(ctx, session.ProjectID, session.ID)
			if err != nil {
				wrapped := fmt.Errorf("reconcile Execution Session %s: %w", session.ID, err)
				if report != nil {
					report(wrapped)
				} else {
					errs = append(errs, wrapped)
				}
				continue
			}
			if process != nil {
				// #11 has no evidence sink yet. Drain recovered streams so an
				// orphaned live process cannot deadlock on output backpressure;
				// #13 replaces these drains with durable output/evidence sinks.
				go func() { _, _ = io.Copy(io.Discard, process.Stdout()) }()
				go func() { _, _ = io.Copy(io.Discard, process.Stderr()) }()
			}
		}
	}
	return errors.Join(errs...)
}

func (s *ExecutionSessionService) Reconcile(ctx context.Context, projectID, sessionID string) (*ExecutionProcess, error) {
	session, err := s.store.GetExecutionSession(ctx, projectID, sessionID)
	if err != nil {
		return nil, translateStoreError(err, "execution_session")
	}
	switch session.Status {
	case "COMPLETED", "FAILED", "CANCELLED":
		return nil, nil
	case "PENDING":
		_, err := s.transition(ctx, session, []string{"PENDING"}, "FAILED", nil)
		return nil, err
	case "STARTING", "RUNNING":
	default:
		return nil, fmt.Errorf("unsupported Execution Session state %q", session.Status)
	}

	instance, err := s.store.GetRuntimeInstance(ctx, projectID, session.RuntimeInstanceID)
	if err != nil {
		return nil, translateStoreError(err, "runtime_instance")
	}
	if instance.Status != string(runtimepkg.StateRunning) {
		_, err := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "FAILED", nil)
		return nil, err
	}

	transport, active, err := s.runners.Reconcile(ctx, projectID, session.RuntimeInstanceID, session.ID)
	if err != nil {
		return nil, NewError("execution_session_uncertain", "Execution Session could not be reconciled with its runner", err)
	}
	if transport == nil {
		_, err := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "FAILED", nil)
		return nil, err
	}
	if active {
		if session.Status == "STARTING" {
			session, err = s.transition(ctx, session, []string{"STARTING"}, "RUNNING", nil)
			if err != nil {
				return nil, err
			}
		}
		if _, err := s.store.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, session.RuntimeInstanceID, "BUSY"); err != nil {
			return nil, translateStoreError(err, "runtime_instance")
		}
		return newExecutionProcess(s, session, transport), nil
	}

	// Health says the process is no longer active, but retained terminal delivery
	// is attached after handshake. Drain output and wait briefly for exit/error so
	// a reconnect race cannot turn a real completion into a false failure.
	go func() { _, _ = io.Copy(io.Discard, transport.Stdout()) }()
	go func() { _, _ = io.Copy(io.Discard, transport.Stderr()) }()
	waitCtx, cancel := context.WithTimeout(ctx, retainedTerminalDeliveryGrace)
	defer cancel()
	result, waitErr := transport.Wait(waitCtx)
	if waitErr == nil {
		exitCode := result.ExitCode
		_, err := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "COMPLETED", &exitCode)
		if err == nil {
			_, err = s.store.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, session.RuntimeInstanceID, "READY")
		}
		return nil, err
	}
	if errors.Is(waitErr, context.Canceled) && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(waitErr, runner.ErrDisconnected) || errors.Is(waitErr, runner.ErrClosed) || errors.Is(waitErr, runner.ErrManagerClosed) {
		return nil, NewError("execution_session_uncertain", "runner disconnected while reconciling the Execution Session", waitErr)
	}
	if errors.Is(waitErr, context.DeadlineExceeded) {
		_, err := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "FAILED", nil)
		if err == nil {
			_, err = s.store.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, session.RuntimeInstanceID, "READY")
		}
		return nil, err
	}
	_, transitionErr := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "FAILED", nil)
	if transitionErr == nil {
		_, transitionErr = s.store.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, session.RuntimeInstanceID, "READY")
	}
	return nil, errors.Join(waitErr, transitionErr)
}
