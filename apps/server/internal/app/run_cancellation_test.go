package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type cancelRunStore struct {
	store.ControlPlaneStore
	run store.Run
	err error
}

func (s *cancelRunStore) GetProject(context.Context, string) (store.Project, error) {
	return store.Project{ID: "project-1"}, nil
}

func (s *cancelRunStore) GetRun(context.Context, string, string) (store.Run, error) {
	return s.run, s.err
}

func TestCancelRunRejectsUnavailableInvalidTerminalAndUnownedRuns(t *testing.T) {
	if err := (&Services{}).CancelRun(t.Context(), "project-1", "run-1"); err == nil {
		t.Fatal("cancellation without services unexpectedly succeeded")
	}

	activeStore := &cancelRunStore{run: store.Run{ID: "run-1", ProjectID: "project-1", Status: "RUNNING"}}
	services := &Services{ControlPlane: New(activeStore), Scheduler: &scheduler.Coordinator{}}
	if err := services.CancelRun(t.Context(), "", "run-1"); err == nil {
		t.Fatal("cancellation with blank project id unexpectedly succeeded")
	}
	if err := services.CancelRun(t.Context(), "project-1", "run-1"); err == nil {
		t.Fatal("unowned run cancellation unexpectedly succeeded")
	}

	activeStore.run.Status = "COMPLETED"
	if err := services.CancelRun(t.Context(), "project-1", "run-1"); err == nil {
		t.Fatal("terminal run cancellation unexpectedly succeeded")
	}

	activeStore.run.Status = "RUNNING"
	activeStore.err = store.ErrNotFound
	if err := services.CancelRun(t.Context(), "project-1", "run-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetRun error=%v, want ErrNotFound", err)
	}
}

type delegationCancelStore struct {
	store.ControlPlaneStore
	runs         map[string]store.Run
	delegations  []store.Delegation
	cancelled    []string
	listErr      error
	getRunCalls  map[string]int
	beforeGetRun func(string, int)
}

func (s *delegationCancelStore) GetProject(context.Context, string) (store.Project, error) {
	return store.Project{ID: "project-1"}, nil
}

func (s *delegationCancelStore) GetRun(_ context.Context, _, id string) (store.Run, error) {
	if s.getRunCalls == nil {
		s.getRunCalls = make(map[string]int)
	}
	s.getRunCalls[id]++
	if s.beforeGetRun != nil {
		s.beforeGetRun(id, s.getRunCalls[id])
	}
	run, ok := s.runs[id]
	if !ok {
		return store.Run{}, store.ErrNotFound
	}
	return run, nil
}

func (s *delegationCancelStore) CancelInactiveRun(_ context.Context, _, id string) (store.RunCancellationResult, error) {
	run, ok := s.runs[id]
	if !ok {
		return store.RunCancellationResult{}, store.ErrNotFound
	}
	if run.Status != "QUEUED" && run.Status != "PAUSED" && run.Status != "STARTING" {
		return store.RunCancellationResult{}, store.ErrConflict
	}
	run.Status = "CANCELLED"
	s.runs[id] = run
	s.cancelled = append(s.cancelled, id)
	return store.RunCancellationResult{Run: run, Event: store.Event{ID: "cancel-" + id, Type: "run.cancelled", ProjectID: run.ProjectID}}, nil
}

func (s *delegationCancelStore) RequestDelegation(context.Context, store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	return store.RequestDelegationResult{}, store.ErrConflict
}
func (s *delegationCancelStore) GetDelegationByRun(context.Context, string, string) (store.Delegation, error) {
	return store.Delegation{}, store.ErrNotFound
}
func (s *delegationCancelStore) ListDelegationsByParentRun(_ context.Context, _, parent string) ([]store.Delegation, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []store.Delegation
	for _, d := range s.delegations {
		if d.ParentRunID == parent {
			out = append(out, d)
		}
	}
	return out, nil
}

func TestCancelRunDurablyCancelsInactiveParentAndQueuedDelegate(t *testing.T) {
	base := &delegationCancelStore{
		runs: map[string]store.Run{
			"parent": {ID: "parent", ProjectID: "project-1", Status: "PAUSED"},
			"child":  {ID: "child", ProjectID: "project-1", Status: "QUEUED"},
		},
		delegations: []store.Delegation{{ID: "delegation-1", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "child"}},
	}
	services := &Services{ControlPlane: New(base), Scheduler: &scheduler.Coordinator{}}
	if err := services.CancelRun(t.Context(), "project-1", "parent"); err != nil {
		t.Fatal(err)
	}
	if got := base.runs["parent"].Status; got != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", got)
	}
	if got := base.runs["child"].Status; got != "CANCELLED" {
		t.Fatalf("child status=%s want CANCELLED", got)
	}
	if len(base.cancelled) != 2 || base.cancelled[0] != "parent" || base.cancelled[1] != "child" {
		t.Fatalf("cancellation order=%v", base.cancelled)
	}
}


type activeDelegationSchedulerStore struct {
	mu         sync.Mutex
	admission  *store.SchedulerAdmission
	admitted   bool
	transition chan store.SchedulerTransition
}

func (s *activeDelegationSchedulerStore) EnqueueJob(context.Context, store.SchedulerJob) (store.SchedulerJob, error) {
	return store.SchedulerJob{}, store.ErrConflict
}

func (s *activeDelegationSchedulerStore) AdmitNextJob(context.Context, string, time.Duration, time.Duration) (*store.SchedulerAdmission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.admitted || s.admission == nil {
		return nil, nil
	}
	s.admitted = true
	copy := *s.admission
	return &copy, nil
}

func (s *activeDelegationSchedulerStore) RenewLease(_ context.Context, _, _, token string, _ time.Duration) (store.SchedulerLease, error) {
	return store.SchedulerLease{LeaseToken: token}, nil
}

func (s *activeDelegationSchedulerStore) TransitionAdmittedJob(_ context.Context, input store.SchedulerTransition) (store.Run, error) {
	s.transition <- input
	run := s.admission.Run
	run.Status = input.RunStatus
	return run, nil
}

func (s *activeDelegationSchedulerStore) ClaimExpiredJobForReconciliation(context.Context, string, time.Duration) (*store.SchedulerAdmission, error) {
	return nil, nil
}

func (s *activeDelegationSchedulerStore) ResolveReconciliation(context.Context, store.SchedulerReconciliation) (store.Run, error) {
	return store.Run{}, store.ErrConflict
}


type preRegistrationDelegationSchedulerStore struct {
	base             *delegationCancelStore
	admission        *store.SchedulerAdmission
	admitted         chan struct{}
	releaseAdmission chan struct{}
	runningAttempted chan struct{}
	runningOnce      sync.Once
	once             sync.Once
}

func (s *preRegistrationDelegationSchedulerStore) EnqueueJob(context.Context, store.SchedulerJob) (store.SchedulerJob, error) {
	return store.SchedulerJob{}, store.ErrConflict
}

func (s *preRegistrationDelegationSchedulerStore) AdmitNextJob(ctx context.Context, _ string, _, _ time.Duration) (*store.SchedulerAdmission, error) {
	var claim *store.SchedulerAdmission
	s.once.Do(func() {
		child := s.base.runs[s.admission.Run.ID]
		child.Status = "STARTING"
		s.base.runs[child.ID] = child
		copy := *s.admission
		copy.Run = child
		claim = &copy
		close(s.admitted)
	})
	if claim == nil {
		return nil, nil
	}
	select {
	case <-s.releaseAdmission:
		return claim, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (*preRegistrationDelegationSchedulerStore) RenewLease(context.Context, string, string, string, time.Duration) (store.SchedulerLease, error) {
	return store.SchedulerLease{}, nil
}

func (s *preRegistrationDelegationSchedulerStore) TransitionAdmittedJob(_ context.Context, input store.SchedulerTransition) (store.Run, error) {
	if input.RunStatus == "RUNNING" {
		s.runningOnce.Do(func() { close(s.runningAttempted) })
	}
	run := s.base.runs[input.RunID]
	if run.Status == "CANCELLED" {
		return store.Run{}, store.ErrConflict
	}
	run.Status = input.RunStatus
	s.base.runs[input.RunID] = run
	return run, nil
}

func (*preRegistrationDelegationSchedulerStore) ClaimExpiredJobForReconciliation(context.Context, string, time.Duration) (*store.SchedulerAdmission, error) {
	return nil, nil
}

func (*preRegistrationDelegationSchedulerStore) ResolveReconciliation(context.Context, store.SchedulerReconciliation) (store.Run, error) {
	return store.Run{}, store.ErrConflict
}

type lifecycleBoundaryCancellationProcessor struct {
	engineEntered chan struct{}
}

func (p *lifecycleBoundaryCancellationProcessor) Process(ctx context.Context, _ *store.SchedulerAdmission, lifecycle scheduler.Lifecycle) (scheduler.Result, error) {
	if _, err := lifecycle.Running(ctx); err != nil {
		return scheduler.Result{}, err
	}
	close(p.engineEntered)
	return scheduler.Result{RunStatus: "COMPLETED"}, nil
}

func TestParentCancellationWinsClaimedDelegatedChildBeforeWorkerRegistration(t *testing.T) {
	base := &delegationCancelStore{
		runs: map[string]store.Run{
			"parent": {ID: "parent", ProjectID: "project-1", Status: "PAUSED"},
			"child":  {ID: "child", ProjectID: "project-1", Status: "QUEUED"},
		},
		delegations: []store.Delegation{{
			ID: "delegation-1", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "child",
		}},
	}
	admission := &store.SchedulerAdmission{
		Job: store.SchedulerJob{ID: "child-job", ProjectID: "project-1", RunID: "child", Kind: "START", State: "CLAIMED"},
		Lease: store.SchedulerLease{JobID: "child-job", LeaseToken: "lease-1"},
		Run: base.runs["child"],
	}
	schedulerStore := &preRegistrationDelegationSchedulerStore{
		base: base, admission: admission, admitted: make(chan struct{}), releaseAdmission: make(chan struct{}),
		runningAttempted: make(chan struct{}),
	}
	processor := &lifecycleBoundaryCancellationProcessor{engineEntered: make(chan struct{})}
	config := scheduler.DefaultConfig("pre-registration-cancel")
	config.PollInterval = 5 * time.Millisecond
	config.LeaseDuration = time.Second
	config.HeartbeatInterval = 100 * time.Millisecond
	config.CapacityBackoff = 5 * time.Millisecond
	config.MaxInFlight = 1
	config.ReportError = func(error) {}
	coordinator, err := scheduler.New(schedulerStore, processor, noOpCancellationReconciler{}, config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- coordinator.Run(ctx) }()

	select {
	case <-schedulerStore.admitted:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not reach claimed pre-registration barrier")
	}
	if got := base.runs["child"].Status; got != "STARTING" {
		t.Fatalf("child status=%s want STARTING after durable admission", got)
	}

	services := &Services{ControlPlane: New(base), Scheduler: coordinator}
	if err := services.CancelRun(t.Context(), "project-1", "parent"); err != nil {
		t.Fatal(err)
	}
	if base.runs["parent"].Status != "CANCELLED" || base.runs["child"].Status != "CANCELLED" {
		t.Fatalf("cancellation statuses parent=%s child=%s", base.runs["parent"].Status, base.runs["child"].Status)
	}
	if base.delegations[0].ContinuationJobID != nil {
		t.Fatalf("cancelled parent received continuation: %+v", base.delegations[0])
	}

	close(schedulerStore.releaseAdmission)
	select {
	case <-schedulerStore.runningAttempted:
	case <-time.After(time.Second):
		t.Fatal("stale claimed child did not reach the fenced RUNNING transition")
	}
	select {
	case <-processor.engineEntered:
		t.Fatal("claimed delegated child entered Engine execution after parent cancellation")
	default:
	}
	if base.runs["parent"].Status != "CANCELLED" {
		t.Fatalf("stale worker resurrected parent to %s", base.runs["parent"].Status)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}

type blockingCancellationProcessor struct {
	started chan struct{}
	once    sync.Once
}

func (p *blockingCancellationProcessor) Process(ctx context.Context, _ *store.SchedulerAdmission, _ scheduler.Lifecycle) (scheduler.Result, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return scheduler.Result{}, ctx.Err()
}

type noOpCancellationReconciler struct{}

func (noOpCancellationReconciler) Reconcile(context.Context, *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
	return store.SchedulerReconciliationUnknown, nil, nil
}

func TestCancelRunCancelsRunningDelegateThroughSchedulerBoundary(t *testing.T) {
	base := &delegationCancelStore{
		runs: map[string]store.Run{
			"parent": {ID: "parent", ProjectID: "project-1", Status: "PAUSED"},
			"child":  {ID: "child", ProjectID: "project-1", Status: "RUNNING"},
		},
		delegations: []store.Delegation{{ID: "delegation-1", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "child"}},
	}
	admission := &store.SchedulerAdmission{
		Job:   store.SchedulerJob{ID: "child-job", ProjectID: "project-1", RunID: "child", Kind: "START", State: "CLAIMED"},
		Lease: store.SchedulerLease{JobID: "child-job", LeaseToken: "lease-1"},
		Run:   base.runs["child"],
	}
	schedulerStore := &activeDelegationSchedulerStore{admission: admission, transition: make(chan store.SchedulerTransition, 1)}
	processor := &blockingCancellationProcessor{started: make(chan struct{})}
	coordinator, err := scheduler.New(schedulerStore, processor, noOpCancellationReconciler{}, scheduler.Config{
		OwnerID: "test-owner", PollInterval: 5 * time.Millisecond, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, CapacityBackoff: 5 * time.Millisecond, MaxInFlight: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- coordinator.Run(ctx) }()
	select {
	case <-processor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not start delegated child")
	}

	services := &Services{ControlPlane: New(base), Scheduler: coordinator}
	if err := services.CancelRun(t.Context(), "project-1", "parent"); err != nil {
		t.Fatal(err)
	}
	if got := base.runs["parent"].Status; got != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", got)
	}
	if len(base.cancelled) != 1 || base.cancelled[0] != "parent" {
		t.Fatalf("inactive cancellation unexpectedly handled running child: %v", base.cancelled)
	}
	select {
	case transition := <-schedulerStore.transition:
		if transition.RunID != "child" || transition.RunStatus != "CANCELLED" {
			t.Fatalf("child transition=%+v want CANCELLED", transition)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("running delegated child was not cancelled through scheduler")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop")
	}
}


func TestCancelRunRetriesDelegatedChildrenAfterParentIsAlreadyCancelled(t *testing.T) {
	base := &delegationCancelStore{
		runs: map[string]store.Run{
			"parent": {ID: "parent", ProjectID: "project-1", Status: "PAUSED"},
			"child":  {ID: "child", ProjectID: "project-1", Status: "RUNNING"},
		},
		delegations: []store.Delegation{{ID: "delegation-1", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "child"}},
	}
	services := &Services{ControlPlane: New(base), Scheduler: &scheduler.Coordinator{}}

	if err := services.CancelRun(t.Context(), "project-1", "parent"); err == nil {
		t.Fatal("first cancellation unexpectedly completed while delegated child was actively owned elsewhere")
	}
	if got := base.runs["parent"].Status; got != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED after partial propagation", got)
	}
	if got := base.runs["child"].Status; got != "RUNNING" {
		t.Fatalf("child status=%s want RUNNING before retry", got)
	}

	child := base.runs["child"]
	child.Status = "QUEUED"
	base.runs["child"] = child
	if err := services.CancelRun(t.Context(), "project-1", "parent"); err != nil {
		t.Fatalf("retry cancelled parent: %v", err)
	}
	if got := base.runs["parent"].Status; got != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", got)
	}
	if got := base.runs["child"].Status; got != "CANCELLED" {
		t.Fatalf("child status=%s want CANCELLED after retry", got)
	}
}

func TestCancelRunRetryTraversesCancelledDelegatedChildToUnfinishedGrandchild(t *testing.T) {
	cancelledOutcome := store.DelegationOutcomeCancelled
	base := &delegationCancelStore{
		runs: map[string]store.Run{
			"root":       {ID: "root", ProjectID: "project-1", Status: "PAUSED"},
			"child":      {ID: "child", ProjectID: "project-1", Status: "QUEUED"},
			"grandchild": {ID: "grandchild", ProjectID: "project-1", Status: "RUNNING"},
		},
		delegations: []store.Delegation{
			{ID: "root-child", ProjectID: "project-1", ParentRunID: "root", DelegatedRunID: "child"},
			{ID: "child-grandchild", ProjectID: "project-1", ParentRunID: "child", DelegatedRunID: "grandchild"},
		},
	}
	services := &Services{ControlPlane: New(base), Scheduler: &scheduler.Coordinator{}}

	if err := services.CancelRun(t.Context(), "project-1", "root"); err == nil {
		t.Fatal("first root cancellation unexpectedly completed while grandchild was actively owned elsewhere")
	}
	if got := base.runs["root"].Status; got != "CANCELLED" {
		t.Fatalf("root status=%s want CANCELLED after partial propagation", got)
	}
	if got := base.runs["child"].Status; got != "CANCELLED" {
		t.Fatalf("child status=%s want CANCELLED after partial propagation", got)
	}
	if got := base.runs["grandchild"].Status; got != "RUNNING" {
		t.Fatalf("grandchild status=%s want RUNNING before retry", got)
	}

	// The real delegated-child terminal transaction records this outcome before
	// descendant propagation can fail. Root retry must still traverse the
	// CANCELLED child rather than treating the recorded outcome as delivery proof.
	base.delegations[0].Outcome = &cancelledOutcome
	grandchild := base.runs["grandchild"]
	grandchild.Status = "QUEUED"
	base.runs["grandchild"] = grandchild

	if err := services.CancelRun(t.Context(), "project-1", "root"); err != nil {
		t.Fatalf("retry cancelled root: %v", err)
	}
	if got := base.runs["grandchild"].Status; got != "CANCELLED" {
		t.Fatalf("grandchild status=%s want CANCELLED after root retry", got)
	}
}

func TestCancelDelegatedChildrenTreatsConcurrentChildCompletionAsSuccessfulPropagation(t *testing.T) {
	base := &delegationCancelStore{
		runs: map[string]store.Run{
			"parent": {ID: "parent", ProjectID: "project-1", Status: "PAUSED"},
			"child":  {ID: "child", ProjectID: "project-1", Status: "QUEUED"},
		},
		delegations: []store.Delegation{{ID: "delegation-1", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "child"}},
	}
	base.beforeGetRun = func(id string, call int) {
		if id != "child" || call != 2 {
			return
		}
		child := base.runs[id]
		child.Status = "COMPLETED"
		base.runs[id] = child
	}
	services := &Services{ControlPlane: New(base), Scheduler: &scheduler.Coordinator{}}

	if err := services.CancelRun(t.Context(), "project-1", "parent"); err != nil {
		t.Fatalf("parent cancellation lost child completion race: %v", err)
	}
	if got := base.runs["parent"].Status; got != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", got)
	}
	if got := base.runs["child"].Status; got != "COMPLETED" {
		t.Fatalf("child status=%s want COMPLETED", got)
	}
}

func TestCancelRunForUserAuthorizesWorkflowMutationBeforeCanonicalCancellation(t *testing.T) {
	if err := (&Services{}).CancelRunForUser(t.Context(), activeProjectActor("member", store.DeploymentRoleMember), "project-1", "run-1"); err == nil {
		t.Fatal("cancellation without Project access unexpectedly succeeded")
	}

	base := &delegationCancelStore{runs: map[string]store.Run{
		"run-1": {ID: "run-1", ProjectID: "project-1", Status: "QUEUED"},
	}}
	accessStore := &projectAccessServiceStore{roles: map[string]string{
		"project-1:viewer": store.ProjectRoleViewer,
		"project-1:member": store.ProjectRoleMember,
	}}
	services := &Services{
		ControlPlane:   New(base),
		Scheduler:      &scheduler.Coordinator{},
		ProjectAccess: newProjectAccessServiceForTest(t, accessStore),
	}
	if err := services.CancelRunForUser(t.Context(), activeProjectActor("viewer", store.DeploymentRoleMember), "project-1", "run-1"); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer cancellation error=%v", err)
	}
	if got := base.runs["run-1"].Status; got != "QUEUED" {
		t.Fatalf("unauthorized cancellation changed Run to %s", got)
	}
	if err := services.CancelRunForUser(t.Context(), activeProjectActor("member", store.DeploymentRoleMember), "project-1", "run-1"); err != nil {
		t.Fatal(err)
	}
	if got := base.runs["run-1"].Status; got != "CANCELLED" {
		t.Fatalf("authorized cancellation status=%s want CANCELLED", got)
	}
}

func TestCancelDelegatedChildrenSkipsTerminalChildrenRegardlessOfOutcome(t *testing.T) {
	outcome := store.DelegationOutcomeSucceeded
	base := &delegationCancelStore{
		runs: map[string]store.Run{
			"parent":            {ID: "parent", ProjectID: "project-1", Status: "PAUSED"},
			"handled-terminal":  {ID: "handled-terminal", ProjectID: "project-1", Status: "COMPLETED"},
			"terminal-child":    {ID: "terminal-child", ProjectID: "project-1", Status: "FAILED"},
		},
		delegations: []store.Delegation{
			{ID: "handled", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "handled-terminal", Outcome: &outcome},
			{ID: "terminal", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "terminal-child"},
		},
	}
	services := &Services{ControlPlane: New(base), Scheduler: &scheduler.Coordinator{}}
	if err := services.CancelRun(t.Context(), "project-1", "parent"); err != nil {
		t.Fatal(err)
	}
	if len(base.cancelled) != 1 || base.cancelled[0] != "parent" {
		t.Fatalf("cancelled Runs=%v want only parent", base.cancelled)
	}
}

func TestCancelDelegatedChildrenPropagatesLookupErrors(t *testing.T) {
	listErr := errors.New("list delegations")
	base := &delegationCancelStore{
		runs:    map[string]store.Run{"parent": {ID: "parent", ProjectID: "project-1", Status: "PAUSED"}},
		listErr: listErr,
	}
	services := &Services{ControlPlane: New(base), Scheduler: &scheduler.Coordinator{}}
	if err := services.CancelRun(t.Context(), "project-1", "parent"); !errors.Is(err, listErr) {
		t.Fatalf("list error=%v want %v", err, listErr)
	}

	base = &delegationCancelStore{
		runs: map[string]store.Run{"parent": {ID: "parent", ProjectID: "project-1", Status: "PAUSED"}},
		delegations: []store.Delegation{{ID: "missing", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "missing-child"}},
	}
	services = &Services{ControlPlane: New(base), Scheduler: &scheduler.Coordinator{}}
	if err := services.CancelRun(t.Context(), "project-1", "parent"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child lookup error=%v want ErrNotFound", err)
	}
}
