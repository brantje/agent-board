package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAgentIssueStatusMutationRequiresMatchingRunningRun(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-run-fence")
	actor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	mutation := store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "IN_PROGRESS",
		Actor:       actor,
		RunID:       &f.run.ID,
		AgentID:     &f.agent.ID,
		WorkspaceID: &f.workspace.ID,
	}

	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("queued Run mutation error=%v want conflict", err)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 0)

	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='RUNNING' WHERE project_id=$1 AND id=$2`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("set fixture Run status: %v", err)
	}
	wrongAgent := "not-the-bound-agent"
	mutation.AgentID = &wrongAgent
	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("mismatched Agent mutation error=%v want conflict", err)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 0)

	mutation.AgentID = &f.agent.ID
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='FAILED' WHERE project_id=$1 AND id=$2`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("set terminal fixture Run status: %v", err)
	}
	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("terminal Run mutation error=%v want conflict", err)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 0)
}

func TestRecoveredAgentIssueStatusMutationFencesLaterIssueMutations(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-recovery-fence")
	agentActor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	humanActor, err := json.Marshal(map[string]string{"type": store.ActorTypeHuman, "id": "human-1"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		Status:    "BACKLOG",
		Actor:     humanActor,
	}); err != nil {
		t.Fatalf("pre-run human mutation: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE runs
		SET status='RUNNING', started_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("start fixture Run: %v", err)
	}

	mutation := store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "IN_PROGRESS",
		Actor:       agentActor,
		RunID:       &f.run.ID,
		AgentID:     &f.agent.ID,
		WorkspaceID: &f.workspace.ID,
		Recovery:    true,
	}
	if _, err := s.SetIssueStatus(ctx, mutation); err != nil {
		t.Fatalf("recovery after pre-run mutation: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "IN_PROGRESS")

	mutation.Recovery = false
	mutation.Status = "REVIEW"
	if _, err := s.SetIssueStatus(ctx, mutation); err != nil {
		t.Fatalf("same-run agent mutation: %v", err)
	}
	mutation.Recovery = true
	mutation.Status = "DONE"
	if _, err := s.SetIssueStatus(ctx, mutation); err != nil {
		t.Fatalf("recovery after same-run mutation: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "DONE")

	if _, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		Status:    "TODO",
		Actor:     humanActor,
	}); err != nil {
		t.Fatalf("later human mutation: %v", err)
	}
	mutation.Status = "BLOCKED"
	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale recovery error=%v want conflict", err)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
}
