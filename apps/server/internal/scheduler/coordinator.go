package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	defaultPollInterval      = 250 * time.Millisecond
	defaultLeaseDuration     = 30 * time.Second
	defaultHeartbeatInterval = 10 * time.Second
	defaultCapacityBackoff   = time.Second
	defaultMaxInFlight       = 32
)

type Config struct {
	OwnerID           string
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	CapacityBackoff   time.Duration
	MaxInFlight       int
	ReportError       func(error)
}

func DefaultConfig(ownerID string) Config {
	return Config{
		OwnerID:           ownerID,
		PollInterval:      defaultPollInterval,
		LeaseDuration:     defaultLeaseDuration,
		HeartbeatInterval: defaultHeartbeatInterval,
		CapacityBackoff:   defaultCapacityBackoff,
		MaxInFlight:       defaultMaxInFlight,
	}
}

type Result struct {
	RunStatus     string
	FailureReason *string
}

type Lifecycle interface {
	Running(context.Context) (store.Run, error)
}

type Processor interface {
	Process(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error)
}

type Reconciler interface {
	Reconcile(context.Context, *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error)
}

type Coordinator struct {
	store      store.SchedulerStore
	processor  Processor
	reconciler Reconciler
	config     Config
}

func New(s store.SchedulerStore, processor Processor, reconciler Reconciler, config Config) (*Coordinator, error) {
	if s == nil || processor == nil || reconciler == nil {
		return nil, fmt.Errorf("scheduler: store, processor and reconciler are required")
	}
	if strings.TrimSpace(config.OwnerID) == "" {
		return nil, fmt.Errorf("scheduler: owner id is required")
	}
	if config.PollInterval <= 0 || config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 || config.CapacityBackoff <= 0 {
		return nil, fmt.Errorf("scheduler: durations must be positive")
	}
	if config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("scheduler: heartbeat interval must be shorter than lease duration")
	}
	if config.MaxInFlight < 1 {
		return nil, fmt.Errorf("scheduler: max in-flight must be at least 1")
	}
	if config.ReportError == nil {
		config.ReportError = func(error) {}
	}
	return &Coordinator{store: s, processor: processor, reconciler: reconciler, config: config}, nil
}

func (c *Coordinator) Run(ctx context.Context) error {
	var workers sync.WaitGroup
	slots := make(chan struct{}, c.config.MaxInFlight)
	defer workers.Wait()

	for {
		if ctx.Err() != nil {
			return nil
		}

		reconciled, err := c.reconcileOne(ctx)
		if err != nil {
			c.config.ReportError(err)
			if !waitFor(ctx, c.config.PollInterval) {
				return nil
			}
			continue
		}
		if reconciled {
			continue
		}

		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return nil
		}

		admission, err := c.store.AdmitNextJob(ctx, c.config.OwnerID, c.config.LeaseDuration, c.config.CapacityBackoff)
		if err != nil {
			<-slots
			c.config.ReportError(err)
			if !waitFor(ctx, c.config.PollInterval) {
				return nil
			}
			continue
		}
		if admission == nil {
			<-slots
			if !waitFor(ctx, c.config.PollInterval) {
				return nil
			}
			continue
		}

		workers.Add(1)
		go func(claim *store.SchedulerAdmission) {
			defer workers.Done()
			defer func() { <-slots }()
			c.process(ctx, claim)
		}(admission)
	}
}

func (c *Coordinator) reconcileOne(ctx context.Context) (bool, error) {
	claim, err := c.store.ClaimExpiredJobForReconciliation(ctx, c.config.OwnerID, c.config.LeaseDuration)
	if err != nil || claim == nil {
		return false, err
	}

	outcome, failureReason, reconcileErr := c.reconciler.Reconcile(ctx, claim)
	if reconcileErr != nil {
		c.config.ReportError(reconcileErr)
		outcome = store.SchedulerReconciliationUnknown
		failureReason = nil
	}
	if outcome == "" {
		outcome = store.SchedulerReconciliationUnknown
	}

	_, err = c.store.ResolveReconciliation(ctx, store.SchedulerReconciliation{
		ProjectID:     claim.Job.ProjectID,
		JobID:         claim.Job.ID,
		RunID:         claim.Run.ID,
		LeaseToken:    claim.Lease.LeaseToken,
		Outcome:       outcome,
		FailureReason: failureReason,
	})
	return true, err
}

func (c *Coordinator) process(parent context.Context, claim *store.SchedulerAdmission) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	heartbeatDone := make(chan struct{})
	var leaseLost atomic.Bool
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		ticker := time.NewTicker(c.config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := c.store.RenewLease(ctx, claim.Job.ProjectID, claim.Job.ID, claim.Lease.LeaseToken, c.config.LeaseDuration); err != nil {
					c.config.ReportError(err)
					leaseLost.Store(true)
					cancel()
					return
				}
			}
		}
	}()

	lifecycle := claimLifecycle{store: c.store, claim: claim}
	result, err := c.processor.Process(ctx, claim, lifecycle)
	close(heartbeatDone)
	heartbeat.Wait()
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			c.config.ReportError(err)
		}
		return
	}
	if leaseLost.Load() {
		return
	}
	if !isFinalProcessorStatus(result.RunStatus) {
		c.config.ReportError(fmt.Errorf("scheduler: processor returned unsupported final status %q", result.RunStatus))
		return
	}

	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(parent), c.config.LeaseDuration)
	defer persistCancel()
	_, err = c.store.TransitionAdmittedJob(persistCtx, store.SchedulerTransition{
		ProjectID:     claim.Job.ProjectID,
		JobID:         claim.Job.ID,
		RunID:         claim.Run.ID,
		LeaseToken:    claim.Lease.LeaseToken,
		RunStatus:     result.RunStatus,
		FailureReason: result.FailureReason,
	})
	if err != nil {
		c.config.ReportError(err)
	}
}

type claimLifecycle struct {
	store store.SchedulerStore
	claim *store.SchedulerAdmission
}

func (l claimLifecycle) Running(ctx context.Context) (store.Run, error) {
	return l.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  l.claim.Job.ProjectID,
		JobID:      l.claim.Job.ID,
		RunID:      l.claim.Run.ID,
		LeaseToken: l.claim.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	})
}

func isFinalProcessorStatus(status string) bool {
	switch status {
	case "WAITING_FOR_INPUT", "PAUSED", "READY_FOR_REVIEW", "COMPLETED", "FAILED", "CANCELLED":
		return true
	default:
		return false
	}
}

func waitFor(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
