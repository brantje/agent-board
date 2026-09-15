package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSetIssueAssigneeTreatsReadyForReviewRunAsActive(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	project, issue, agent, firstRun := assignedReadyForReviewRun(t, s, "ready-review")

	if _, err := s.SetIssueAssignee(ctx, project.ID, issue.ID, nil, store.EmptyObject); err != nil {
		t.Fatalf("unassign issue: %v", err)
	}
	result, err := s.SetIssueAssignee(ctx, project.ID, issue.ID, &store.Assignee{Type: "AGENT", ID: agent.ID}, store.EmptyObject)
	if err != nil {
		t.Fatalf("repeat assignment: %v", err)
	}
	for _, event := range result.Events {
		if event.Type == "run.created" {
			t.Fatalf("repeat assignment created run event: %+v", event)
		}
	}

	runs, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	var issueRuns []store.Run
	for _, run := range runs {
		if run.IssueID == issue.ID {
			issueRuns = append(issueRuns, run)
		}
	}
	if len(issueRuns) != 1 || issueRuns[0].ID != firstRun.ID {
		t.Fatalf("runs=%+v, want only existing %s", issueRuns, firstRun.ID)
	}
}

func TestSetIssueAssigneeSuppressesDuplicateWithOlderReadyForReview(t *testing.T) {
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

	if _, err := s.SetIssueAssignee(ctx, project.ID, issue.ID, nil, store.EmptyObject); err != nil {
		t.Fatalf("unassign issue: %v", err)
	}
	result, err := s.SetIssueAssignee(ctx, project.ID, issue.ID, &store.Assignee{Type: "AGENT", ID: agent.ID}, store.EmptyObject)
	if err != nil {
		t.Fatalf("reassign after failed follow-up: %v", err)
	}
	for _, event := range result.Events {
		if event.Type == "run.created" {
			t.Fatalf("reassignment created duplicate run event: %+v", event)
		}
	}

	var jobCount int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_jobs WHERE run_id=$1 AND kind='START' AND state='QUEUED'`, firstRun.ID).Scan(&jobCount); err != nil {
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
		t.Fatalf("review run status=%s, want READY_FOR_REVIEW", previous.Status)
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
		Engine:           "scripted",
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

	if _, err := s.SetIssueAssignee(ctx, project.ID, issue.ID, &store.Assignee{Type: "AGENT", ID: agent.ID}, store.EmptyObject); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	runs, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	var firstRun store.Run
	for _, run := range runs {
		if run.IssueID == issue.ID {
			firstRun = run
			break
		}
	}
	if firstRun.ID == "" {
		t.Fatal("automatic assignment did not create a Run")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='READY_FOR_REVIEW', updated_at=now() WHERE id=$1`, firstRun.ID); err != nil {
		t.Fatalf("mark run ready for review: %v", err)
	}
	firstRun.Status = "READY_FOR_REVIEW"
	return project, issue, agent, firstRun
}
