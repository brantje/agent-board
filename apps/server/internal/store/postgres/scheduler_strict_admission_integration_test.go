package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestStrictOrderSkipsDeferredBoardHead(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "strict-deferred")
	enableStrictOrder(t, s, f.project.ID)
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)

	head := enqueueFixtureRun(t, s, f, f.run, "strict-deferred-head")
	nextRun := createQueuedFixtureRun(t, s, f, "strict-deferred-next")
	next := enqueueFixtureRun(t, s, f, nextRun, "strict-deferred-next")
	setBoardOrder(t, s, f.project.ID, f.issue.ID, nextRun.IssueID)
	if _, err := s.pool.Exec(ctx, `UPDATE scheduler_jobs SET available_at=now()+interval '1 hour' WHERE id=$1`, head.ID); err != nil {
		t.Fatal(err)
	}

	admission, err := s.AdmitNextJob(ctx, "strict-deferred-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != next.ID {
		t.Fatalf("admitted job=%s want next eligible=%s", admission.Job.ID, next.ID)
	}
}

func TestStrictOrderSkipsClaimedRunningBoardHead(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "strict-claimed")
	enableStrictOrder(t, s, f.project.ID)
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)

	head := enqueueFixtureRun(t, s, f, f.run, "strict-claimed-head")
	nextRun := createQueuedFixtureRun(t, s, f, "strict-claimed-next")
	next := enqueueFixtureRun(t, s, f, nextRun, "strict-claimed-next")
	setBoardOrder(t, s, f.project.ID, f.issue.ID, nextRun.IssueID)
	if _, err := s.pool.Exec(ctx, `UPDATE scheduler_jobs SET state='CLAIMED' WHERE id=$1`, head.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='STARTING' WHERE id=$1`, f.run.ID); err != nil {
		t.Fatal(err)
	}

	admission, err := s.AdmitNextJob(ctx, "strict-claimed-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != next.ID {
		t.Fatalf("admitted job=%s want next eligible=%s", admission.Job.ID, next.ID)
	}
}

func TestStrictOrderSkipsWorkspaceIneligibleBoardHead(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "strict-workspace")
	enableStrictOrder(t, s, f.project.ID)
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)

	head := enqueueFixtureRun(t, s, f, f.run, "strict-workspace-head")
	peer, err := s.CreateRun(ctx, store.Run{
		ProjectID: f.project.ID, IssueID: f.issue.ID, WorkspaceID: f.workspace.ID,
		AgentID: &f.agent.ID, Attempt: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	peerJob := enqueueFixtureRun(t, s, f, peer, "strict-workspace-owner")
	if _, err := s.pool.Exec(ctx, `UPDATE scheduler_jobs SET state='CLAIMED' WHERE id=$1`, peerJob.ID); err != nil {
		t.Fatal(err)
	}

	nextRun := createQueuedFixtureRun(t, s, f, "strict-workspace-next")
	next := enqueueFixtureRun(t, s, f, nextRun, "strict-workspace-next")
	setBoardOrder(t, s, f.project.ID, f.issue.ID, nextRun.IssueID)

	admission, err := s.AdmitNextJob(ctx, "strict-workspace-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != next.ID {
		t.Fatalf("admitted job=%s want next eligible=%s", admission.Job.ID, next.ID)
	}
	assertSchedulerWaitReason(t, s, head.ID, store.SchedulerWaitWorkspace)
}

func TestStrictOrderSkipsCapacityIneligibleBoardHead(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "strict-capacity")
	setSchedulerLimits(t, s, f.agent.ID, 1, f.model.ID, nil)

	consumer := enqueueFixtureRun(t, s, f, f.run, "strict-capacity-consumer")
	admitted, err := s.AdmitNextJob(ctx, "strict-capacity-consumer-worker", time.Minute, time.Second)
	if err != nil || admitted == nil || admitted.Job.ID != consumer.ID {
		t.Fatalf("consumer admission=%+v err=%v", admitted, err)
	}

	enableStrictOrder(t, s, f.project.ID)
	headRun := createQueuedFixtureRun(t, s, f, "strict-capacity-head")
	head := enqueueFixtureRun(t, s, f, headRun, "strict-capacity-head")

	otherAgent := f.agent
	otherAgent.ID = ""
	otherAgent.Name = "strict-capacity-other"
	otherAgent.ConcurrencyLimit = 1
	otherAgent, err = s.CreateAgent(ctx, otherAgent)
	if err != nil {
		t.Fatal(err)
	}
	nextRun := createQueuedFixtureRun(t, s, f, "strict-capacity-next")
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET agent_id=$2 WHERE id=$1`, nextRun.ID, otherAgent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='AGENT', assignee_id=$2 WHERE id=$1`, nextRun.IssueID, otherAgent.ID); err != nil {
		t.Fatal(err)
	}
	next := enqueueFixtureRun(t, s, f, nextRun, "strict-capacity-next")
	setBoardOrder(t, s, f.project.ID, headRun.IssueID, nextRun.IssueID)

	admission, err := s.AdmitNextJob(ctx, "strict-capacity-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != next.ID {
		t.Fatalf("admitted job=%s want next eligible=%s", admission.Job.ID, next.ID)
	}
	assertSchedulerWaitReason(t, s, head.ID, store.SchedulerWaitAgentCapacity)
}

func TestStrictOrderSkipsRunnerIneligibleBoardHead(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "strict-runner")
	enableStrictOrder(t, s, f.project.ID)
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)

	rows, err := s.pool.Query(ctx, `SELECT id::text FROM runners WHERE deleted_at IS NULL AND revoked_at IS NULL`)
	if err != nil {
		t.Fatal(err)
	}
	var runnerIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		runnerIDs = append(runnerIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agents SET engine='no-live-runner' WHERE id=$1`, f.agent.ID); err != nil {
		t.Fatal(err)
	}
	s.SetRunnerCandidates(func(engine string) []string {
		if engine == "no-live-runner" {
			return nil
		}
		return append([]string(nil), runnerIDs...)
	})

	head := enqueueFixtureRun(t, s, f, f.run, "strict-runner-head")
	otherAgent := f.agent
	otherAgent.ID = ""
	otherAgent.Name = "strict-runner-other"
	otherAgent.Engine = "scripted"
	otherAgent.ConcurrencyLimit = 4
	otherAgent, err = s.CreateAgent(ctx, otherAgent)
	if err != nil {
		t.Fatal(err)
	}
	nextRun := createQueuedFixtureRun(t, s, f, "strict-runner-next")
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET agent_id=$2 WHERE id=$1`, nextRun.ID, otherAgent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='AGENT', assignee_id=$2 WHERE id=$1`, nextRun.IssueID, otherAgent.ID); err != nil {
		t.Fatal(err)
	}
	next := enqueueFixtureRun(t, s, f, nextRun, "strict-runner-next")
	setBoardOrder(t, s, f.project.ID, f.issue.ID, nextRun.IssueID)

	admission, err := s.AdmitNextJob(ctx, "strict-runner-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != next.ID {
		t.Fatalf("admitted job=%s want next eligible=%s", admission.Job.ID, next.ID)
	}
	assertSchedulerWaitReason(t, s, head.ID, "runner_capacity")
}

func TestSchedulerTreatsInvalidStoredStrictOrderAsDisabled(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "strict-invalid-stored")
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)
	if _, err := s.pool.Exec(ctx, `UPDATE projects SET workflow_settings='{"strictOrder":"banana"}'::jsonb WHERE id=$1`, f.project.ID); err != nil {
		t.Fatal(err)
	}

	first := enqueueFixtureRun(t, s, f, f.run, "strict-invalid-first")
	secondRun := createQueuedFixtureRun(t, s, f, "strict-invalid-second")
	enqueueFixtureRun(t, s, f, secondRun, "strict-invalid-second")
	setBoardOrder(t, s, f.project.ID, secondRun.IssueID, f.issue.ID)

	admission, err := s.AdmitNextJob(ctx, "strict-invalid-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != first.ID {
		t.Fatalf("invalid stored setting changed legacy scheduling: got=%s want=%s", admission.Job.ID, first.ID)
	}
}

func enableStrictOrder(t *testing.T, s *Store, projectID string) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `UPDATE projects SET workflow_settings=jsonb_set(workflow_settings, '{strictOrder}', 'true'::jsonb, true) WHERE id=$1`, projectID); err != nil {
		t.Fatal(err)
	}
}

func setBoardOrder(t *testing.T, s *Store, projectID string, issueIDs ...string) {
	t.Helper()
	ctx := context.Background()
	for position, issueID := range issueIDs {
		if _, err := s.pool.Exec(ctx, `UPDATE issues SET status='TODO', board_position=$3 WHERE project_id=$1 AND id=$2`, projectID, issueID, position); err != nil {
			t.Fatal(err)
		}
	}
}

func assertSchedulerWaitReason(t *testing.T, s *Store, jobID, want string) {
	t.Helper()
	var reason *string
	if err := s.pool.QueryRow(context.Background(), `SELECT wait_reason FROM scheduler_jobs WHERE id=$1`, jobID).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason == nil || *reason != want {
		t.Fatalf("job %s wait reason=%v want=%q", jobID, reason, want)
	}
}
