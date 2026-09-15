package postgres

import (
	"fmt"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueEnqueueMutationMatrix(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "matrix")
	statuses := []string{"BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE"}
	for _, status := range statuses {
		t.Run("assignment/"+status, func(t *testing.T) {
			i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: status, Status: status})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: a.ID}, store.EmptyObject)
			if err != nil {
				t.Fatal(err)
			}
			if result.Issue.Status != status {
				t.Fatalf("status changed to %s", result.Issue.Status)
			}
			want := 0
			if status != "BACKLOG" {
				want = 1
			}
			assertIssueEnqueueCounts(t, s, p.ID, i.ID, want)
			if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: a.ID}, store.EmptyObject); err != nil {
				t.Fatal(err)
			}
			assertIssueEnqueueCounts(t, s, p.ID, i.ID, want)
		})
		for _, next := range statuses {
			t.Run(status+"/"+next, func(t *testing.T) {
				i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "transition", Status: status})
				if err != nil {
					t.Fatal(err)
				}
				// Seed ownership independently to isolate the status-change trigger.
				if _, err = s.pool.Exec(ctx, `UPDATE issues SET assignee_type='AGENT',assignee_id=$2 WHERE id=$1`, i.ID, a.ID); err != nil {
					t.Fatal(err)
				}
				i.Status = next
				if _, err = s.UpdateIssue(ctx, i); err != nil {
					t.Fatal(err)
				}
				want := 0
				if status == "BACKLOG" && next != "BACKLOG" && next != "DONE" {
					want = 1
				}
				assertIssueEnqueueCounts(t, s, p.ID, i.ID, want)
				if _, err = s.UpdateIssue(ctx, i); err != nil {
					t.Fatal(err)
				}
				assertIssueEnqueueCounts(t, s, p.ID, i.ID, want)
			})
		}
	}
}

func assertIssueEnqueueCounts(t *testing.T, s *Store, pid, iid string, want int) {
	t.Helper()
	var runs, jobs, events, cancelled int
	err := s.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM runs WHERE issue_id=$1), (SELECT count(*) FROM scheduler_jobs j JOIN runs r ON r.id=j.run_id WHERE r.issue_id=$1), (SELECT count(*) FROM events WHERE issue_id=$1 AND type='run.created'), (SELECT count(*) FROM events WHERE issue_id=$1 AND type='run.cancelled')`, iid).Scan(&runs, &jobs, &events, &cancelled)
	if err != nil {
		t.Fatal(err)
	}
	if runs != want || jobs != want || events != want || cancelled != 0 {
		t.Fatalf("runs=%d jobs=%d created=%d cancelled=%d want=%d", runs, jobs, events, cancelled, want)
	}
	var invalidEvidence int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM events e JOIN runs r ON r.id=e.run_id WHERE e.issue_id=$1 AND e.type='run.created' AND (e.project_id <> $2::uuid OR e.agent_id IS DISTINCT FROM r.agent_id OR e.workspace_id IS DISTINCT FROM r.workspace_id OR e.sequence IS DISTINCT FROM 1 OR e.payload->>'status' IS DISTINCT FROM 'QUEUED' OR (e.payload->>'attempt')::int IS DISTINCT FROM r.attempt)`, iid, pid).Scan(&invalidEvidence); err != nil || invalidEvidence != 0 {
		t.Fatalf("invalid creation evidence=%d err=%v", invalidEvidence, err)
	}
}

func TestIssueEnqueueCoexistenceAndRaces(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "races")
	b := a
	b.ID = ""
	b.Name = "B"
	b, err := s.CreateAgent(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "race", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for n := 0; n < 24; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := a.ID
			if n%2 == 1 {
				id = b.ID
			}
			_, err := s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: id}, store.EmptyObject)
			errs <- err
		}(n)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertIssueEnqueueCounts(t, s, p.ID, i.ID, 2)
	runs, err := s.ListRuns(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var workspace string
	for _, r := range runs {
		if r.IssueID != i.ID {
			continue
		}
		if r.Status != "QUEUED" {
			t.Fatalf("run changed: %+v", r)
		}
		if workspace != "" && workspace != r.WorkspaceID {
			t.Fatal("separate Workspaces")
		}
		workspace = r.WorkspaceID
	}
	// Status and clearing ownership never cancel existing execution.
	i.Status = "DONE"
	if _, err = s.UpdateIssue(ctx, i); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	assertIssueEnqueueCounts(t, s, p.ID, i.ID, 2)
	var active int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE issue_id=$1 AND status='QUEUED'`, i.ID).Scan(&active); err != nil || active != 2 {
		t.Fatalf("active=%d err=%v", active, err)
	}
}

func TestIssueEnqueueUnrunnableAndCreate(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "readiness")
	for n, disabled := range []bool{false, true} {
		if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=$2 WHERE id=$1`, a.ModelProfileID, !disabled); err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"BACKLOG", "TODO", "DONE"} {
			i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: fmt.Sprint(n, status), Status: status, AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if !disabled && status != "BACKLOG" {
				want = 1
			}
			assertIssueEnqueueCounts(t, s, p.ID, i.ID, want)
			if i.AssigneeID == nil || *i.AssigneeID != a.ID {
				t.Fatal("ownership lost")
			}
		}
	}
}

func TestIssueEnqueueActiveStatesAndUserOwnership(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "active-states")
	b := a
	b.ID = ""
	b.Name = "other"
	b, err := s.CreateAgent(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	uinput := authUser("owner", "owner@example.com", store.UserStatusActive)
	uinput.DeploymentRole = store.DeploymentRoleAdmin
	u, err := s.CreateUser(ctx, uinput)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range activeRunStatuses {
		i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: status, Status: "TODO", AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.pool.Exec(ctx, `UPDATE runs SET status=$2 WHERE issue_id=$1`, i.ID, status); err != nil {
			t.Fatal(err)
		}
		for _, target := range []*store.Assignee{{Type: "AGENT", ID: b.ID}, {Type: "AGENT", ID: a.ID}, {Type: "USER", ID: u.ID}, nil} {
			if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, target, store.EmptyObject); err != nil {
				t.Fatal(err)
			}
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 2)
		var actual string
		if err = s.pool.QueryRow(ctx, `SELECT status FROM runs WHERE issue_id=$1 AND agent_id=$2`, i.ID, a.ID).Scan(&actual); err != nil || actual != status {
			t.Fatalf("status=%s want=%s err=%v", actual, status, err)
		}
	}
}

func TestIssueEnqueueStatusAssignmentRace(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "mixed-race")
	for n := 0; n < 10; n++ {
		i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "mixed", Status: "BACKLOG"})
		if err != nil {
			t.Fatal(err)
		}
		errs := make(chan error, 2)
		go func() {
			_, err := s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: a.ID}, store.EmptyObject)
			errs <- err
		}()
		go func() { next := i; next.Status = "TODO"; _, err := s.UpdateIssue(ctx, next); errs <- err }()
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 1)
	}
}

func TestIssueEnqueueEventFailureRollsBackMutationAndQueue(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "rollback")
	i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "atomic", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION reject_run_created() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.type='run.created' THEN RAISE EXCEPTION 'event rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_run_created BEFORE INSERT ON events FOR EACH ROW EXECUTE FUNCTION reject_run_created()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: a.ID}, store.EmptyObject); err == nil {
		t.Fatal("expected event failure")
	}
	assertIssueEnqueueCounts(t, s, p.ID, i.ID, 0)
	got, err := s.GetIssue(ctx, p.ID, i.ID)
	if err != nil || got.AssigneeID != nil {
		t.Fatalf("ownership committed: %+v %v", got, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE issue_id=$1`, i.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("workspace committed: %d %v", count, err)
	}
}

func TestIssueEnqueueUnrunnableMutationAndRetryAfterCompletion(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	p, _, a, _ := assignedReadyForReviewRun(t, s, "skip")
	for _, sql := range []string{`UPDATE model_profiles SET enabled=false`, `UPDATE providers SET enabled=false`, `UPDATE providers SET health_status='UNHEALTHY'`} {
		if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true; UPDATE providers SET enabled=true,health_status='UNKNOWN'`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
		i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "unready", Status: "BACKLOG"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: a.ID}, store.EmptyObject); err != nil {
			t.Fatal(err)
		}
		i.Status = "TODO"
		if _, err = s.UpdateIssue(ctx, i); err != nil {
			t.Fatal(err)
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 0)
		if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, nil, store.EmptyObject); err != nil {
			t.Fatal(err)
		}
		result, err := s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: a.ID}, store.EmptyObject)
		if err != nil || result.Issue.AssigneeID == nil {
			t.Fatalf("ownership failed: %+v %v", result.Issue, err)
		}
		assertIssueEnqueueCounts(t, s, p.ID, i.ID, 0)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true; UPDATE providers SET enabled=true,health_status='UNKNOWN'`); err != nil {
		t.Fatal(err)
	}
	i, err := s.CreateIssue(ctx, store.Issue{ProjectID: p.ID, Title: "retry", Status: "TODO", AssigneeType: stringPtrPG("AGENT"), AssigneeID: &a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE runs SET status='COMPLETED' WHERE issue_id=$1`, i.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: a.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	assertIssueEnqueueCounts(t, s, p.ID, i.ID, 1)
	if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetIssueAssignee(ctx, p.ID, i.ID, &store.Assignee{Type: "AGENT", ID: a.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	assertIssueEnqueueCounts(t, s, p.ID, i.ID, 2)
}
