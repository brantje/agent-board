package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueExecutionRejectsUnregisteredEngineAndRecoversOnCorrection(t *testing.T) {
	s := New(testPool(t))
	s.SetEngineRegistered(func(name string) bool { return name == "scripted" })
	ctx := t.Context()
	project, _, agent, _ := assignedReadyForReviewRun(t, s, "engine-recovery")

	agent.Engine = "unregistered-engine"
	if _, err := s.UpdateAgent(ctx, agent.ProjectID, agent); err != nil {
		t.Fatalf("set invalid Engine: %v", err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "invalid engine", Status: "TODO"})
	if err != nil {
		t.Fatalf("create Issue: %v", err)
	}
	result, err := s.SetIssueAssignee(ctx, project.ID, issue.ID, &store.Assignee{Type: "AGENT", ID: agent.ID}, store.EmptyObject)
	if err != nil {
		t.Fatalf("assign invalid Engine Agent: %v", err)
	}
	if result.Issue.AssigneeID == nil || *result.Issue.AssigneeID != agent.ID {
		t.Fatalf("ownership was not preserved: %+v", result.Issue)
	}
	assertIssueEnqueueCounts(t, s, project.ID, issue.ID, 0)

	state, err := s.GetIssueExecutionState(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatalf("execution state: %v", err)
	}
	if state.State != store.IssueExecutionConfigurationUnavailable || state.CanStart {
		t.Fatalf("execution state=%+v", state)
	}
	if _, _, err := s.StartIssueRun(ctx, project.ID, issue.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("StartIssueRun error=%v want conflict", err)
	}

	// Runner presence/capacity is scheduler admission policy, not Run-creation
	// configuration. Correcting only the Engine must therefore queue a Run even
	// when no Runner candidates are available.
	s.SetRunnerCandidates(func(string) []string { return nil })
	agent.Engine = "scripted"
	service := app.New(s)
	if _, err := service.UpdateAgent(ctx, agent.ProjectID, agent); err != nil {
		t.Fatalf("correct Engine: %v", err)
	}
	assertIssueEnqueueCounts(t, s, project.ID, issue.ID, 1)

	runs, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatalf("list Runs: %v", err)
	}
	var queued *store.Run
	for i := range runs {
		if runs[i].IssueID == issue.ID {
			queued = &runs[i]
			break
		}
	}
	if queued == nil || queued.Status != "QUEUED" {
		t.Fatalf("queued Run=%+v", queued)
	}
	var reservations int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_capacity_reservations WHERE run_id=$1`, queued.ID).Scan(&reservations); err != nil {
		t.Fatalf("count reservations: %v", err)
	}
	if reservations != 0 {
		t.Fatalf("Run creation reserved scheduler capacity: %d", reservations)
	}

	agent.Name += " renamed"
	if _, err := service.UpdateAgent(ctx, agent.ProjectID, agent); err != nil {
		t.Fatalf("non-readiness Agent update: %v", err)
	}
	assertIssueEnqueueCounts(t, s, project.ID, issue.ID, 1)
}
