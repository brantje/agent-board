package postgres

import (
    "context"
    "testing"
    "time"

    "github.com/brantje/agent-board/apps/server/internal/scheduler"
    "github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerDefersReassignedRunWhileWorkspaceClaimIsOwned(t *testing.T) {
    s := New(testPool(t))
    ctx := context.Background()
    f := seedRunFixture(t, s, "workspace-reassign")
    setSchedulerLimits(t, s, f.agent.ID, 2, f.model.ID, intPtr(2))
    enqueueFixtureRun(t, s, f, f.run, "workspace-owner")

    owner, err := s.AdmitNextJob(ctx, "worker-a", time.Minute, time.Millisecond)
    if err != nil || owner == nil {
        t.Fatalf("owner admission=%+v err=%v", owner, err)
    }

    other := f.agent
    other.ID = ""
    other.Name = "workspace-other"
    other.ConcurrencyLimit = 2
    other, err = s.CreateAgent(ctx, other)
    if err != nil {
        t.Fatal(err)
    }
    if _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "AGENT", ID: other.ID}, store.EmptyObject); err != nil {
        t.Fatal(err)
    }

    waiting, err := s.AdmitNextJob(ctx, "worker-b", time.Minute, time.Millisecond)
    if err != nil {
        t.Fatal(err)
    }
    if waiting != nil {
        t.Fatalf("second Run admitted while Workspace owned: %+v", waiting)
    }

    var runID, status string
    var reason *string
    if err := s.pool.QueryRow(ctx, `SELECT id::text,status,queue_reason FROM runs WHERE issue_id=$1 AND agent_id=$2`, f.issue.ID, other.ID).Scan(&runID, &status, &reason); err != nil {
        t.Fatal(err)
    }
    if status != "QUEUED" || reason == nil || *reason != store.SchedulerWaitWorkspace {
        t.Fatalf("waiting Run status=%s reason=%v", status, reason)
    }
    var reservations int
    if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_capacity_reservations WHERE run_id=$1`, runID).Scan(&reservations); err != nil {
        t.Fatal(err)
    }
    if reservations != 0 {
        t.Fatalf("waiting Run reservations=%d want 0", reservations)
    }
    ownerRun, err := s.GetRun(ctx, f.project.ID, f.run.ID)
    if err != nil || ownerRun.Status != "STARTING" {
        t.Fatalf("owner Run=%+v err=%v", ownerRun, err)
    }

    if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
        ProjectID: f.project.ID, JobID: owner.Job.ID, RunID: owner.Run.ID,
        LeaseToken: owner.Lease.LeaseToken, RunStatus: "COMPLETED",
    }); err != nil {
        t.Fatal(err)
    }
    time.Sleep(2 * time.Millisecond)
    next, err := s.AdmitNextJob(ctx, "worker-b", time.Minute, time.Millisecond)
    if err != nil || next == nil || next.Run.ID != runID {
        t.Fatalf("deferred Run did not execute next: %+v err=%v", next, err)
    }
}

func TestSchedulerKeepsWorkspaceOwnedByLiveQuestionSession(t *testing.T) {
    s := New(testPool(t))
    ctx := context.Background()
    f := seedRunFixture(t, s, "workspace-question")
    setSchedulerLimits(t, s, f.agent.ID, 2, f.model.ID, intPtr(2))
    enqueueFixtureRun(t, s, f, f.run, "question-owner")
    owner, err := s.AdmitNextJob(ctx, "worker-a", time.Minute, time.Millisecond)
    if err != nil || owner == nil {
        t.Fatalf("owner admission=%+v err=%v", owner, err)
    }
    session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{ProjectID: f.project.ID, RunID: f.run.ID, RunnerID: owner.RunnerID, Status: "RUNNING"})
    if err != nil {
        t.Fatalf("create live session: %v", err)
    }
    if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
        ProjectID: f.project.ID, JobID: owner.Job.ID, RunID: owner.Run.ID,
        LeaseToken: owner.Lease.LeaseToken, RunStatus: "WAITING_FOR_INPUT",
    }); err != nil {
        t.Fatal(err)
    }

    other := f.agent
    other.ID = ""
    other.Name = "question-other"
    other.ConcurrencyLimit = 2
    other, err = s.CreateAgent(ctx, other)
    if err != nil { t.Fatal(err) }
    if _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "AGENT", ID: other.ID}, store.EmptyObject); err != nil { t.Fatal(err) }
    time.Sleep(2 * time.Millisecond)
    if next, err := s.AdmitNextJob(ctx, "worker-b", time.Minute, time.Millisecond); err != nil || next != nil {
        t.Fatalf("live Question session lost Workspace ownership: next=%+v err=%v", next, err)
    }

    if _, err := s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{ProjectID: f.project.ID, SessionID: session.ID, FromStatuses: []string{"RUNNING"}, Status: "COMPLETED"}); err != nil {
        t.Fatal(err)
    }
    time.Sleep(2 * time.Millisecond)
    if next, err := s.AdmitNextJob(ctx, "worker-b", time.Minute, time.Millisecond); err != nil || next == nil {
        t.Fatalf("Run was not admitted after session release: next=%+v err=%v", next, err)
    }
}


type workspaceBoundaryProcessor struct {
	processed chan string
}

func (p workspaceBoundaryProcessor) Process(ctx context.Context, claim *store.SchedulerAdmission, lifecycle scheduler.Lifecycle) (scheduler.Result, error) {
	if _, err := lifecycle.Running(ctx); err != nil {
		return scheduler.Result{}, err
	}
	p.processed <- claim.Run.ID
	return scheduler.Result{RunStatus: "COMPLETED"}, nil
}

type workspaceBoundaryReconciler struct{}

func (workspaceBoundaryReconciler) Reconcile(context.Context, *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
	return store.SchedulerReconciliationUnknown, nil, nil
}

func TestCoordinatorDoesNotInvokeProcessorUntilWorkspaceOwnershipIsReleased(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "workspace-boundary")
	setSchedulerLimits(t, s, f.agent.ID, 2, f.model.ID, intPtr(2))
	enqueueFixtureRun(t, s, f, f.run, "workspace-boundary-owner")

	owner, err := s.AdmitNextJob(ctx, "workspace-owner", time.Minute, time.Millisecond)
	if err != nil || owner == nil {
		t.Fatalf("owner admission=%+v err=%v", owner, err)
	}
	session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID:     f.run.ID,
		RunnerID:  owner.RunnerID,
		Status:    "RUNNING",
	})
	if err != nil {
		t.Fatalf("create owner session: %v", err)
	}
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      owner.Job.ID,
		RunID:      owner.Run.ID,
		LeaseToken: owner.Lease.LeaseToken,
		RunStatus:  "WAITING_FOR_INPUT",
	}); err != nil {
		t.Fatalf("owner wait transition: %v", err)
	}

	other := f.agent
	other.ID = ""
	other.Name = "workspace-boundary-other"
	other.ConcurrencyLimit = 2
	other, err = s.CreateAgent(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "AGENT", ID: other.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	var queuedRunID string
	if err := s.pool.QueryRow(ctx, `SELECT id::text FROM runs WHERE issue_id=$1 AND agent_id=$2`, f.issue.ID, other.ID).Scan(&queuedRunID); err != nil {
		t.Fatal(err)
	}

	processed := make(chan string, 1)
	config := scheduler.DefaultConfig("workspace-boundary-worker")
	config.PollInterval = 5 * time.Millisecond
	config.LeaseDuration = 500 * time.Millisecond
	config.HeartbeatInterval = 100 * time.Millisecond
	config.CapacityBackoff = 5 * time.Millisecond
	config.MaxInFlight = 1
	coordinator, err := scheduler.New(s, workspaceBoundaryProcessor{processed: processed}, workspaceBoundaryReconciler{}, config)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- coordinator.Run(runCtx) }()

	select {
	case runID := <-processed:
		cancel()
		<-done
		t.Fatalf("processor invoked while Workspace remained owned: %s", runID)
	case <-time.After(50 * time.Millisecond):
	}
	queued, err := s.GetRun(ctx, f.project.ID, queuedRunID)
	if err != nil || queued.Status != "QUEUED" || queued.QueueReason == nil || *queued.QueueReason != store.SchedulerWaitWorkspace {
		cancel()
		<-done
		t.Fatalf("queued Run during Workspace ownership=%+v err=%v", queued, err)
	}
	ownerRun, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil || ownerRun.Status != "WAITING_FOR_INPUT" {
		cancel()
		<-done
		t.Fatalf("owner Run changed while peer waited=%+v err=%v", ownerRun, err)
	}

	if _, err := s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID:   f.project.ID,
		SessionID:   session.ID,
		FromStatuses: []string{"RUNNING"},
		Status:      "COMPLETED",
	}); err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}

	select {
	case runID := <-processed:
		if runID != queuedRunID {
			cancel()
			<-done
			t.Fatalf("processed Run=%s want %s", runID, queuedRunID)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("queued Run never reached processor after Workspace release")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		completed, err := s.GetRun(ctx, f.project.ID, queuedRunID)
		if err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		if completed.Status == "COMPLETED" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatalf("processed Run never completed: %+v", completed)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("coordinator shutdown: %v", err)
	}
}
