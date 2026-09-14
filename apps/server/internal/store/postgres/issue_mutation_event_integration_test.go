package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCreateIssueRollsBackWhenCausalEventFails(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	project, err := s.CreateProject(ctx, testProjectInput("causal-create", "/repo/causal-create", prefixForTestName("causal-create")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `
		CREATE FUNCTION reject_issue_created() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.type='issue.created' THEN RAISE EXCEPTION 'issue event rejected'; END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER reject_issue_created BEFORE INSERT ON events FOR EACH ROW EXECUTE FUNCTION reject_issue_created()
	`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "atomic create", Status: "TODO"}); err == nil {
		t.Fatal("expected issue.created failure")
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE project_id=$1`, project.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("issue committed without causal event: count=%d", count)
	}
}

func TestUpdateIssueRollsBackAutoEnqueueWhenCausalEventFails(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	fixture := seedRunFixture(t, s, "causal-update")
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID:    fixture.project.ID,
		Title:        "atomic status",
		Status:       "BACKLOG",
		AssigneeType: stringPtrPG("AGENT"),
		AssigneeID:   &fixture.agent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `
		CREATE FUNCTION reject_issue_status_changed() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.type='issue.status_changed' THEN RAISE EXCEPTION 'issue event rejected'; END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER reject_issue_status_changed BEFORE INSERT ON events FOR EACH ROW EXECUTE FUNCTION reject_issue_status_changed()
	`); err != nil {
		t.Fatal(err)
	}
	issue.Status = "TODO"
	if _, err = s.UpdateIssue(ctx, issue); err == nil {
		t.Fatal("expected issue.status_changed failure")
	}
	current, err := s.GetIssue(ctx, fixture.project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "BACKLOG" {
		t.Fatalf("status committed without causal event: %s", current.Status)
	}
	var runs, jobs, runEvents int
	if err = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM runs WHERE issue_id=$1),
			(SELECT count(*) FROM scheduler_jobs j JOIN runs r ON r.id=j.run_id WHERE r.issue_id=$1),
			(SELECT count(*) FROM events WHERE issue_id=$1 AND type='run.created')
	`, issue.ID).Scan(&runs, &jobs, &runEvents); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || jobs != 0 || runEvents != 0 {
		t.Fatalf("auto-enqueue escaped rollback: runs=%d jobs=%d run.created=%d", runs, jobs, runEvents)
	}
}
