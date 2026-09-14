package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAssignIssueTreatsReadyForReviewRunAsActive(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	project, issue, agent, firstRun := assignedReadyForReviewRun(t, s, "ready-review")

	_, repeatedRun, err := s.AssignIssue(ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatalf("repeat assignment: %v", err)
	}
	if repeatedRun.ID != firstRun.ID {
		t.Fatalf("repeat assignment created run %s, want existing %s", repeatedRun.ID, firstRun.ID)
	}

	var runCount int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE project_id=$1 AND issue_id=$2`, project.ID, issue.ID).Scan(&runCount); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("run count=%d, want 1", runCount)
	}
}

func TestAssignIssueSuppressesDuplicateWithOlderReadyForReview(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	project, issue, agent, firstRun := assignedReadyForReviewRun(t, s, "failed-follow-up")

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status, failure_reason, completed_at)
		SELECT project_id, issue_id, workspace_id, agent_id, 2, 'FAILED', 'engine failed', now()
		FROM runs WHERE id=$1
	`, firstRun.ID); err != nil {
		t.Fatalf("insert failed follow-up run: %v", err)
	}

	_, nextRun, err := s.AssignIssue(ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatalf("reassign after failed follow-up: %v", err)
	}
	if nextRun.ID != firstRun.ID {
		t.Fatalf("reassign run=%+v, want existing active review Run", nextRun)
	}

	var jobCount int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_jobs WHERE run_id=$1 AND kind='START' AND state='QUEUED'`, nextRun.ID).Scan(&jobCount); err != nil {
		t.Fatalf("count start jobs: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("queued start jobs=%d, want 1", jobCount)
	}

	previous, err := s.GetRun(ctx, project.ID, firstRun.ID)
	if err != nil {
		t.Fatalf("get previous review run: %v", err)
	}
	if previous.Status != "READY_FOR_REVIEW" {
		t.Fatalf("superseded review run status=%s, want READY_FOR_REVIEW", previous.Status)
	}
}

func assignedReadyForReviewRun(t *testing.T, s *Store, name string) (store.Project, store.Issue, store.Agent, store.Run) {
	t.Helper()
	ctx := t.Context()

	project, err := s.CreateProject(ctx, testProjectInput(name, "/repo/"+name, prefixForTestName(name)))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	provider, err := s.CreateProvider(ctx, store.Provider{Name: name, Kind: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID:  &project.ID,
		ProviderID: provider.ID,
		Name:       name,
		Model:      "test",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create model profile: %v", err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{
		ProjectID:        &project.ID,
		Name:             name,
		Engine:           "test",
		ModelProfileID:   model.ID,
		EngineSettings:   store.EmptyObject,
		ConcurrencyLimit: 1,
		State:            "ENABLED",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: name, Status: "TODO"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	_, firstRun, err := s.AssignIssue(ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='READY_FOR_REVIEW', updated_at=now() WHERE id=$1`, firstRun.ID); err != nil {
		t.Fatalf("mark run ready for review: %v", err)
	}
	return project, issue, agent, firstRun
}
