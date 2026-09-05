package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestNewValidatesCoordinatorConfig(t *testing.T) {
	base := DefaultConfig("worker")
	processor := processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		return Result{RunStatus: "COMPLETED"}, nil
	})
	reconciler := testReconciler()

	if _, err := New(nil, processor, reconciler, base); err == nil {
		t.Fatal("expected missing store error")
	}
	if _, err := New(&fakeSchedulerStore{}, nil, reconciler, base); err == nil {
		t.Fatal("expected missing processor error")
	}
	if _, err := New(&fakeSchedulerStore{}, processor, nil, base); err == nil {
		t.Fatal("expected missing reconciler error")
	}

	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "owner", mutate: func(c *Config) { c.OwnerID = "" }},
		{name: "poll", mutate: func(c *Config) { c.PollInterval = 0 }},
		{name: "lease", mutate: func(c *Config) { c.LeaseDuration = 0 }},
		{name: "heartbeat", mutate: func(c *Config) { c.HeartbeatInterval = c.LeaseDuration }},
		{name: "backoff", mutate: func(c *Config) { c.CapacityBackoff = 0 }},
		{name: "inflight", mutate: func(c *Config) { c.MaxInFlight = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if _, err := New(&fakeSchedulerStore{}, processor, reconciler, cfg); err == nil {
				t.Fatal("expected invalid config error")
			}
		})
	}
}

func TestCoordinatorReconcilesBeforeNewAdmission(t *testing.T) {
	reconcileClaim := fakeAdmission("reconcile-job")
	queuedClaim := fakeAdmission("queued-job")
	fs := &fakeSchedulerStore{
		reconciliationClaims: []*store.SchedulerAdmission{reconcileClaim},
		admissions:           []*store.SchedulerAdmission{queuedClaim},
		transitioned:         make(chan store.SchedulerTransition, 1),
	}
	processor := processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		return Result{RunStatus: "COMPLETED"}, nil
	})
	reconciler := reconcilerFunc(func(context.Context, *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
		return store.SchedulerReconciliationRetry, nil, nil
	})
	cfg := testConfig()
	c, err := New(fs, processor, reconciler, cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case transition := <-fs.transitioned:
		if transition.JobID != queuedClaim.Job.ID || transition.RunStatus != "COMPLETED" {
			t.Fatalf("transition=%+v", transition)
		}
	case <-time.After(time.Second):
		t.Fatal("coordinator did not process queued admission")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run coordinator: %v", err)
	}

	events := fs.eventsSnapshot()
	if indexOf(events, "resolve-reconciliation") == -1 || indexOf(events, "admit") == -1 {
		t.Fatalf("events=%v", events)
	}
	if indexOf(events, "resolve-reconciliation") > indexOf(events, "admit") {
		t.Fatalf("reconciliation must precede admission: %v", events)
	}
}

func TestCoordinatorLifecycleMarksRunningThenFinal(t *testing.T) {
	claim := fakeAdmission("lifecycle-job")
	fs := &fakeSchedulerStore{}
	processor := processorFunc(func(ctx context.Context, got *store.SchedulerAdmission, lifecycle Lifecycle) (Result, error) {
		if got.Job.ID != claim.Job.ID {
			t.Fatalf("claim=%s want %s", got.Job.ID, claim.Job.ID)
		}
		run, err := lifecycle.Running(ctx)
		if err != nil {
			return Result{}, err
		}
		if run.Status != "RUNNING" {
			t.Fatalf("running result=%s", run.Status)
		}
		return Result{RunStatus: "READY_FOR_REVIEW"}, nil
	})
	c, err := New(fs, processor, testReconciler(), testConfig())
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	c.process(context.Background(), claim)

	transitions := fs.transitionsSnapshot()
	if len(transitions) != 2 || transitions[0].RunStatus != "RUNNING" || transitions[1].RunStatus != "READY_FOR_REVIEW" {
		t.Fatalf("transitions=%+v", transitions)
	}
	for _, transition := range transitions {
		if transition.LeaseToken != claim.Lease.LeaseToken {
			t.Fatalf("transition lost lease fencing: %+v", transition)
		}
	}
}

func TestCoordinatorRenewsLeaseWhileProcessorRuns(t *testing.T) {
	claim := fakeAdmission("heartbeat-job")
	fs := &fakeSchedulerStore{renewed: make(chan struct{}, 1)}
	processor := processorFunc(func(ctx context.Context, _ *store.SchedulerAdmission, _ Lifecycle) (Result, error) {
		select {
		case <-fs.renewed:
			return Result{RunStatus: "COMPLETED"}, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	})
	cfg := testConfig()
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.HeartbeatInterval = 5 * time.Millisecond
	c, err := New(fs, processor, testReconciler(), cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c.process(ctx, claim)
	if fs.renewCountSnapshot() == 0 {
		t.Fatal("expected lease heartbeat")
	}
	transitions := fs.transitionsSnapshot()
	if len(transitions) != 1 || transitions[0].RunStatus != "COMPLETED" {
		t.Fatalf("transitions=%+v", transitions)
	}
}

func TestCoordinatorLeavesAmbiguousProcessorFailureForReconciliation(t *testing.T) {
	claim := fakeAdmission("processor-error")
	reported := make(chan error, 1)
	fs := &fakeSchedulerStore{}
	processor := processorFunc(func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error) {
		return Result{}, errors.New("transport disappeared")
	})
	cfg := testConfig()
	cfg.ReportError = func(err error) { reported <- err }
	c, err := New(fs, processor, testReconciler(), cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	c.process(context.Background(), claim)

	if len(fs.transitionsSnapshot()) != 0 {
		t.Fatal("ambiguous processor failure must not create a terminal transition")
	}
	select {
	case <-reported:
	case <-time.After(time.Second):
		t.Fatal("expected processor error report")
	}
}

func TestCoordinatorCancelsProcessorWhenLeaseHeartbeatIsLost(t *testing.T) {
	claim := fakeAdmission("lost-lease")
	fs := &fakeSchedulerStore{renewErr: store.ErrNotFound}
	processorCancelled := make(chan struct{}, 1)
	processor := processorFunc(func(ctx context.Context, _ *store.SchedulerAdmission, _ Lifecycle) (Result, error) {
		<-ctx.Done()
		processorCancelled <- struct{}{}
		return Result{}, ctx.Err()
	})
	cfg := testConfig()
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.HeartbeatInterval = 5 * time.Millisecond
	c, err := New(fs, processor, testReconciler(), cfg)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c.process(ctx, claim)
	select {
	case <-processorCancelled:
	case <-time.After(time.Second):
		t.Fatal("processor was not cancelled after lease loss")
	}
	if len(fs.transitionsSnapshot()) != 0 {
		t.Fatal("lease loss must not transition with stale ownership")
	}
}

func testConfig() Config {
	cfg := DefaultConfig("test-worker")
	cfg.PollInterval = time.Millisecond
	cfg.LeaseDuration = time.Second
	cfg.HeartbeatInterval = 100 * time.Millisecond
	cfg.CapacityBackoff = time.Millisecond
	cfg.MaxInFlight = 2
	return cfg
}

func testReconciler() Reconciler {
	return reconcilerFunc(func(context.Context, *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
		return store.SchedulerReconciliationUnknown, nil, nil
	})
}

type processorFunc func(context.Context, *store.SchedulerAdmission, Lifecycle) (Result, error)

func (f processorFunc) Process(ctx context.Context, admission *store.SchedulerAdmission, lifecycle Lifecycle) (Result, error) {
	return f(ctx, admission, lifecycle)
}

type reconcilerFunc func(context.Context, *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error)

func (f reconcilerFunc) Reconcile(ctx context.Context, admission *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
	return f(ctx, admission)
}

type fakeSchedulerStore struct {
	mu                   sync.Mutex
	admissions           []*store.SchedulerAdmission
	reconciliationClaims []*store.SchedulerAdmission
	transitions          []store.SchedulerTransition
	events               []string
	renewCount           int
	renewErr             error
	renewed              chan struct{}
	transitioned         chan store.SchedulerTransition
}

func (f *fakeSchedulerStore) EnqueueJob(context.Context, store.SchedulerJob) (store.SchedulerJob, error) {
	return store.SchedulerJob{}, nil
}

func (f *fakeSchedulerStore) AdmitNextJob(context.Context, string, time.Duration, time.Duration) (*store.SchedulerAdmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "admit")
	if len(f.admissions) == 0 {
		return nil, nil
	}
	claim := f.admissions[0]
	f.admissions = f.admissions[1:]
	return claim, nil
}

func (f *fakeSchedulerStore) RenewLease(context.Context, string, string, string, time.Duration) (store.SchedulerLease, error) {
	f.mu.Lock()
	f.renewCount++
	err := f.renewErr
	renewed := f.renewed
	f.mu.Unlock()
	if renewed != nil {
		select {
		case renewed <- struct{}{}:
		default:
		}
	}
	if err != nil {
		return store.SchedulerLease{}, err
	}
	return store.SchedulerLease{}, nil
}

func (f *fakeSchedulerStore) TransitionAdmittedJob(_ context.Context, input store.SchedulerTransition) (store.Run, error) {
	f.mu.Lock()
	f.transitions = append(f.transitions, input)
	f.events = append(f.events, "transition")
	transitioned := f.transitioned
	f.mu.Unlock()
	if transitioned != nil {
		select {
		case transitioned <- input:
		default:
		}
	}
	return store.Run{ID: input.RunID, ProjectID: input.ProjectID, Status: input.RunStatus}, nil
}

func (f *fakeSchedulerStore) ClaimExpiredJobForReconciliation(context.Context, string, time.Duration) (*store.SchedulerAdmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "claim-reconciliation")
	if len(f.reconciliationClaims) == 0 {
		return nil, nil
	}
	claim := f.reconciliationClaims[0]
	f.reconciliationClaims = f.reconciliationClaims[1:]
	return claim, nil
}

func (f *fakeSchedulerStore) ResolveReconciliation(context.Context, store.SchedulerReconciliation) (store.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "resolve-reconciliation")
	return store.Run{}, nil
}

func (f *fakeSchedulerStore) transitionsSnapshot() []store.SchedulerTransition {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.SchedulerTransition(nil), f.transitions...)
}

func (f *fakeSchedulerStore) eventsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func (f *fakeSchedulerStore) renewCountSnapshot() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renewCount
}

func fakeAdmission(id string) *store.SchedulerAdmission {
	return &store.SchedulerAdmission{
		Job:            store.SchedulerJob{ID: id, ProjectID: "project", RunID: "run-" + id, State: "CLAIMED"},
		Run:            store.Run{ID: "run-" + id, ProjectID: "project", Status: "STARTING"},
		Lease:          store.SchedulerLease{JobID: id, OwnerID: "worker", LeaseToken: "lease-" + id},
		AgentID:        "agent",
		ModelProfileID: "model",
	}
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
