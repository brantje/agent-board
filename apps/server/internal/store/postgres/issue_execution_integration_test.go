package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestStartIssueRunMatrixAndRecovery(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "execution")
	for _, status := range []string{"BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE"} {
		if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, a.ModelProfileID); err != nil {
			t.Fatal(err)
		}
		i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: status, Status: status, AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = s.StartIssueRun(ctx, p.ID, i.ID); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("invalid config/status: %v", err)
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 0)
		if _, err = s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true WHERE id=$1`, a.ModelProfileID); err != nil {
			t.Fatal(err)
		}
		_, _, err = s.StartIssueRun(ctx, p.ID, i.ID)
		if status == "BACKLOG" {
			if !errors.Is(err, store.ErrConflict) {
				t.Fatal(err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = s.StartIssueRun(ctx, p.ID, i.ID); err != nil {
			t.Fatal(err)
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 1)
		got, err := s.GetIssue(ctx, p.ID, i.ID)
		if err != nil || got.Status != status || *got.AssigneeID != a.ID {
			t.Fatalf("mutated issue: %+v %v", got, err)
		}
		if _, err = s.pool.Exec(ctx, `UPDATE runs SET status='COMPLETED' WHERE issue_id=$1`, i.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err = s.StartIssueRun(ctx, p.ID, i.ID); err != nil {
			t.Fatal(err)
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 2)
	}
}

func TestReconcileIssueExecutionRacesAndCurrentOwnership(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "recovery")
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, a.ModelProfileID); err != nil {
		t.Fatal(err)
	}
	var issues []store.Issue
	for _, status := range []string{"BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE", "TODO"} {
		i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: status, Status: status, AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
		if err != nil {
			t.Fatal(err)
		}
		issues = append(issues, i)
	}
	if _, err := s.SetIssueAssignee(ctx, p.ID, issues[6].ID, nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true WHERE id=$1`, a.ModelProfileID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ReconcileIssueExecution(ctx, store.IssueExecutionFilter{})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for n, i := range issues {
		want := 1
		if n == 0 || n == 6 {
			want = 0
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, want)
	}
	if _, _, err := s.StartIssueRun(ctx, p.ID, issues[6].ID); !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	if _, _, err := s.StartIssueRun(ctx, "00000000-0000-4000-8000-000000000001", issues[1].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestExecutionConfigurationChangeReconcilesOnlyAffectedAssignments(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "config-recovery")
	svc := app.New(s)
	for _, kind := range []string{"agent", "model", "provider"} {
		t.Run(kind, func(t *testing.T) {
			model, err := s.GetModelProfile(ctx, &p.ID, a.ModelProfileID)
			if err != nil {
				t.Fatal(err)
			}
			provider, err := s.GetProvider(ctx, &p.ID, model.ProviderID)
			if err != nil {
				t.Fatal(err)
			}
			i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: kind, Status: "BACKLOG", AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "agent":
				a.State = "DISABLED"
				_, err = s.UpdateAgent(ctx, a.ProjectID, a)
			case "model":
				model.Enabled = false
				_, err = s.UpdateModelProfile(ctx, model.ProjectID, model)
			case "provider":
				provider.Enabled = false
				_, err = s.UpdateProvider(ctx, provider.ProjectID, provider)
			}
			if err != nil {
				t.Fatal(err)
			}
			i.Status = "TODO"
			if _, err = s.UpdateIssue(ctx, i); err != nil {
				t.Fatal(err)
			}
			assertIssueEnqueueCounts(t, s, p.ID, i.ID, 0)
			switch kind {
			case "agent":
				a.State = "ENABLED"
				_, err = svc.UpdateAgent(ctx, a.ProjectID, a)
			case "model":
				model.Enabled = true
				_, err = svc.UpdateModelProfile(ctx, model.ProjectID, model)
			case "provider":
				provider.Enabled = true
				_, err = svc.UpdateProvider(ctx, provider.ProjectID, provider)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertIssueEnqueueCounts(t, s, p.ID, i.ID, 1)
			if _, err = s.pool.Exec(ctx, `UPDATE runs SET status='COMPLETED' WHERE issue_id=$1`, i.ID); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "agent":
				a.Name += " renamed"
				_, err = svc.UpdateAgent(ctx, a.ProjectID, a)
			case "model":
				model.Name += " renamed"
				_, err = svc.UpdateModelProfile(ctx, model.ProjectID, model)
			case "provider":
				provider.Name += " renamed"
				_, err = svc.UpdateProvider(ctx, provider.ProjectID, provider)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertIssueEnqueueCounts(t, s, p.ID, i.ID, 1)
			if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, nil, store.EmptyObject); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReconciliationRechecksCandidateOwnership(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "stale-candidate")
	i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "candidate", Status: "BACKLOG", AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.ID = ""
	b.Name = "replacement"
	b, err = s.CreateAgent(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	userInput := authUser("newowner", "newowner@example.com", store.UserStatusActive)
	userInput.DeploymentRole = store.DeploymentRoleAdmin
	u, err := s.CreateUser(ctx, userInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []*store.Assignee{nil, {Type: "USER", ID: u.ID}, {Type: "AGENT", ID: b.ID}} {
		// This is the same boundary as a scan completed before an assignment wins
		// the Issue lock. The stale candidate must never enqueue its former Agent.
		if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, target, store.EmptyObject); err != nil {
			t.Fatal(err)
		}
		if _, err = s.pool.Exec(ctx, `UPDATE issues SET status='TODO' WHERE id=$1`, i.ID); err != nil {
			t.Fatal(err)
		}
		run, event, err := s.enqueueCurrentIssue(ctx, p.ID, i.ID, a.ID, false)
		if err != nil || run.ID != "" || event.ID != "" {
			t.Fatalf("stale work: %+v %+v %v", run, event, err)
		}
		if target == nil || target.Type == "USER" {
			if _, _, err = s.StartIssueRun(ctx, p.ID, i.ID); !errors.Is(err, store.ErrConflict) {
				t.Fatal(err)
			}
		}
		if _, err = s.pool.Exec(ctx, `UPDATE issues SET status='BACKLOG' WHERE id=$1`, i.ID); err != nil {
			t.Fatal(err)
		}
	}
	assertIssueEnqueueCounts(t, s, p.ID, i.ID, 0)
}

func TestStartAndRecoveryEventFailureRollback(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "execution-rollback")
	i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "rollback", Status: "BACKLOG", AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE issues SET status='TODO' WHERE id=$1`, i.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION reject_execution_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.type='run.created' THEN RAISE EXCEPTION 'event rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_execution_event BEFORE INSERT ON events FOR EACH ROW EXECUTE FUNCTION reject_execution_event()`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.StartIssueRun(ctx, p.ID, i.ID); err == nil {
		t.Fatal("missing event failure")
	}
	events, err := s.ReconcileIssueExecution(ctx, store.IssueExecutionFilter{AgentID: a.ID})
	if err == nil || len(events) != 0 {
		t.Fatalf("%+v %v", events, err)
	}
	assertIssueEnqueueCounts(t, s, p.ID, i.ID, 0)
	var workspaces int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE issue_id=$1`, i.ID).Scan(&workspaces); err != nil || workspaces != 0 {
		t.Fatalf("workspace leak: %d %v", workspaces, err)
	}
	if _, err = s.pool.Exec(ctx, `DROP TRIGGER reject_execution_event ON events`); err != nil {
		t.Fatal(err)
	}
	// A fresh service performs the same scan after a missed transition/failure.
	if err = app.New(s).ReconcileIssueExecution(ctx, store.IssueExecutionFilter{}); err != nil {
		t.Fatal(err)
	}
	assertIssueEnqueueCounts(t, s, p.ID, i.ID, 1)
}

func TestStartRecoveryAndMutationRace(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "start-races")
	for range 5 {
		i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "race", Status: "BACKLOG", AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
		if err != nil {
			t.Fatal(err)
		}
		// No live Runner or materialized source is available. Creation still
		// succeeds and stays in the canonical queue alongside other attempts.
		if _, err = s.pool.Exec(ctx, `UPDATE issues SET status='TODO' WHERE id=$1`, i.ID); err != nil {
			t.Fatal(err)
		}
		errs := make(chan error, 3)
		go func() { _, _, err := s.StartIssueRun(ctx, p.ID, i.ID); errs <- err }()
		go func() {
			_, err := s.ReconcileIssueExecution(ctx, store.IssueExecutionFilter{AgentID: a.ID})
			errs <- err
		}()
		go func() { i.Status = "IN_PROGRESS"; _, err := s.UpdateIssue(ctx, i); errs <- err }()
		for range 3 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 1)
		runs, err := s.ListRuns(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range runs {
			if run.IssueID == i.ID && run.Status != "QUEUED" {
				t.Fatal(run)
			}
		}
	}
}

func TestExecutionRecoveryFiltersAndSchedulerBoundary(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "filtered-recovery")
	model, err := s.GetModelProfile(ctx, &p.ID, a.ModelProfileID)
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []store.IssueExecutionFilter{{AgentID: a.ID}, {ModelProfileID: model.ID}, {ProviderID: model.ProviderID}} {
		i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "filtered", Status: "BACKLOG", AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.pool.Exec(ctx, `UPDATE issues SET status='TODO' WHERE id=$1`, i.ID); err != nil {
			t.Fatal(err)
		}
		unmatched := store.IssueExecutionFilter{AgentID: "00000000-0000-4000-8000-000000000001"}
		if events, err := s.ReconcileIssueExecution(ctx, unmatched); err != nil || len(events) != 0 {
			t.Fatalf("unmatched: %+v %v", events, err)
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 0)
		if events, err := s.ReconcileIssueExecution(ctx, filter); err != nil || len(events) != 1 {
			t.Fatalf("matched: %+v %v", events, err)
		}
		// Once a Run is queued, recovery must not even consider it while scheduler
		// availability changes. This holds even when execution config becomes invalid.
		if _, err = s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, model.ID); err != nil {
			t.Fatal(err)
		}
		if events, err := s.ReconcileIssueExecution(ctx, filter); err != nil || len(events) != 0 {
			t.Fatalf("queued reconsidered: %+v %v", events, err)
		}
		if _, _, err = s.StartIssueRun(ctx, p.ID, i.ID); !errors.Is(err, store.ErrConflict) {
			t.Fatal("Start must revalidate configuration", err)
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 1)
		if _, err = s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true WHERE id=$1`, model.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExplicitStartQueuesWhileSchedulerCapacityIsReserved(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "start-capacity")
	setSchedulerLimits(t, s, f.agent.ID, 1, f.model.ID, intPtr(1))
	enqueueFixtureRun(t, s, f, f.run, "occupy-capacity")
	admission := mustAdmit(t, s, "capacity-owner")
	i, err := s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "waiting", Status: "BACKLOG", AssigneeType: stringPtrPG("AGENT"), AssigneeID: &f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE issues SET status='TODO' WHERE id=$1`, i.ID); err != nil {
		t.Fatal(err)
	}
	run, event, err := s.StartIssueRun(ctx, f.project.ID, i.ID)
	if err != nil || run.Status != "QUEUED" || event.Type != "run.created" {
		t.Fatalf("%+v %+v %v", run, event, err)
	}
	if next, err := s.AdmitNextJob(ctx, "waiting-owner", time.Minute, time.Second); err != nil || next != nil {
		t.Fatalf("capacity bypass: %+v %v", next, err)
	}
	got, err := s.GetRun(ctx, f.project.ID, run.ID)
	if err != nil || got.QueueReason == nil || *got.QueueReason != store.SchedulerWaitAgentCapacity {
		t.Fatalf("wait reason: %+v %v", got, err)
	}
	if events, err := s.ReconcileIssueExecution(ctx, store.IssueExecutionFilter{}); err != nil || len(events) != 0 {
		t.Fatalf("scheduler wait scanned: %+v %v", events, err)
	}
	assertIssueEnqueueCounts(t, s, f.project.ID, i.ID, 1)
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 3)
}
