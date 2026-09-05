package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCoordinatorRunReportsReconciliationErrorAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reported := make(chan error, 1)
	fs := &runErrorStore{claimErr: errors.New("reconciliation unavailable")}
	cfg := testConfig()
	cfg.ReportError = func(err error) {
		reported <- err
		cancel()
	}
	c, err := New(fs, noopProcessor(), nil, cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	select {
	case got := <-reported:
		if got.Error() != "reconciliation unavailable" {
			t.Fatalf("reported error=%v", got)
		}
	default:
		t.Fatal("expected reconciliation error report")
	}
}

func TestCoordinatorRunReportsAdmissionErrorAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reported := make(chan error, 1)
	fs := &runErrorStore{admitErr: errors.New("admission unavailable")}
	cfg := testConfig()
	cfg.ReportError = func(err error) {
		reported <- err
		cancel()
	}
	c, err := New(fs, noopProcessor(), nil, cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	select {
	case got := <-reported:
		if got.Error() != "admission unavailable" {
			t.Fatalf("reported error=%v", got)
		}
	default:
		t.Fatal("expected admission error report")
	}
}

func TestCoordinatorRunReturnsImmediatelyForCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := New(&runErrorStore{}, noopProcessor(), nil, testConfig())
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("run cancelled coordinator: %v", err)
	}
}

func TestCoordinatorReconcilerErrorFallsBackToUnknown(t *testing.T) {
	claim := fakeAdmission("reconciler-error")
	capture := &reconciliationCaptureStore{claim: claim}
	reported := make(chan error, 1)
	cfg := testConfig()
	cfg.ReportError = func(err error) { reported <- err }
	c, err := New(capture, noopProcessor(), reconcilerFunc(func(context.Context, *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
		return "", nil, errors.New("inspect failed")
	}), cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	reconciled, err := c.reconcileOne(context.Background())
	if err != nil || !reconciled {
		t.Fatalf("reconcile one reconciled=%v err=%v", reconciled, err)
	}
	if capture.resolved.Outcome != store.SchedulerReconciliationUnknown {
		t.Fatalf("outcome=%s want UNKNOWN", capture.resolved.Outcome)
	}
	select {
	case <-reported:
	default:
		t.Fatal("expected reconciler error report")
	}
}

func TestCoordinatorEmptyReconcilerOutcomeDefaultsToUnknown(t *testing.T) {
	claim := fakeAdmission("empty-outcome")
	capture := &reconciliationCaptureStore{claim: claim}
	c, err := New(capture, noopProcessor(), reconcilerFunc(func(context.Context, *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
		return "", nil, nil
	}), testConfig())
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	reconciled, err := c.reconcileOne(context.Background())
	if err != nil || !reconciled {
		t.Fatalf("reconcile one reconciled=%v err=%v", reconciled, err)
	}
	if capture.resolved.Outcome != store.SchedulerReconciliationUnknown {
		t.Fatalf("outcome=%s want UNKNOWN", capture.resolved.Outcome)
	}
}

func TestCoordinatorRejectsUnsupportedProcessorFinalStatus(t *testing.T) {
	reported := make(chan error, 1)
	fs := &fakeSchedulerStore{}
	cfg := testConfig()
	cfg.ReportError = func(err error) { reported <- err }
	c, err := New(fs, processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		return Result{RunStatus: "RUNNING"}, nil
	}), nil, cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	c.process(context.Background(), fakeAdmission("unsupported-final"))
	if len(fs.transitionsSnapshot()) != 0 {
		t.Fatal("unsupported final status must not transition")
	}
	select {
	case <-reported:
	default:
		t.Fatal("expected unsupported status report")
	}
}

func TestFinalProcessorStatusesAndWaitCancellation(t *testing.T) {
	for _, status := range []string{"WAITING_FOR_INPUT", "PAUSED", "READY_FOR_REVIEW", "COMPLETED", "FAILED", "CANCELLED"} {
		if !isFinalProcessorStatus(status) {
			t.Fatalf("status %s should be final", status)
		}
	}
	if isFinalProcessorStatus("RUNNING") || isFinalProcessorStatus("") {
		t.Fatal("running/empty must not be final processor statuses")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitFor(ctx, time.Hour) {
		t.Fatal("cancelled wait must return false")
	}
}

func noopProcessor() Processor {
	return processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		return Result{RunStatus: "COMPLETED"}, nil
	})
}

type runErrorStore struct {
	store.SchedulerStore
	claimErr error
	admitErr error
}

func (s *runErrorStore) ClaimExpiredJobForReconciliation(context.Context, string, time.Duration) (*store.SchedulerAdmission, error) {
	return nil, s.claimErr
}

func (s *runErrorStore) AdmitNextJob(context.Context, string, time.Duration, time.Duration) (*store.SchedulerAdmission, error) {
	return nil, s.admitErr
}

type reconciliationCaptureStore struct {
	store.SchedulerStore
	claim    *store.SchedulerAdmission
	resolved store.SchedulerReconciliation
}

func (s *reconciliationCaptureStore) ClaimExpiredJobForReconciliation(context.Context, string, time.Duration) (*store.SchedulerAdmission, error) {
	claim := s.claim
	s.claim = nil
	return claim, nil
}

func (s *reconciliationCaptureStore) ResolveReconciliation(_ context.Context, input store.SchedulerReconciliation) (store.Run, error) {
	s.resolved = input
	return inputRun(input), nil
}

func inputRun(input store.SchedulerReconciliation) store.Run {
	return store.Run{ID: input.RunID, ProjectID: input.ProjectID}
}
