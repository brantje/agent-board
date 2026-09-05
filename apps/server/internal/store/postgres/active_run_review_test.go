package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAssignIssueTreatsReadyForReviewRunAsActive(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()

	project, err := s.CreateProject(ctx, store.Project{Name: "ready-review", RepositoryPath: "/repo/ready-review"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	provider, err := s.CreateProvider(ctx, store.Provider{Name: "ready-review", Kind: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID:  &project.ID,
		ProviderID: provider.ID,
		Name:       "ready-review",
		Model:      "test",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create model profile: %v", err)
	}
	runtime, err := s.CreateRuntime(ctx, store.Runtime{
		ProjectID:     &project.ID,
		Name:          "ready-review",
		Kind:          "docker",
		Image:         "agent-board:test",
		NetworkPolicy: "none",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	executor, err := s.CreateExecutorProfile(ctx, store.ExecutorProfile{
		ProjectID:      &project.ID,
		Name:           "ready-review",
		Engine:         "test",
		ModelProfileID: model.ID,
		RuntimeID:      runtime.ID,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("create executor profile: %v", err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{
		ProjectID:         &project.ID,
		Name:              "ready-review",
		ExecutorProfileID: executor.ID,
		ConcurrencyLimit:  1,
		State:             "ENABLED",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "ready-review", Status: "TODO"})
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
