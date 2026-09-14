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
		Recovery:    true,
	}

	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("queued Run recovery error=%v want conflict", err)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 0)

	if _, err := s.pool.Exec(ctx, `
		UPDATE runs
		SET status='RUNNING', started_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("set fixture Run status: %v", err)
	}
	wrongAgent := "not-the-bound-agent"
	mutation.AgentID = &wrongAgent
	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("mismatched Agent recovery error=%v want conflict", err)
	}

	mutation.AgentID = &f.agent.ID
	wrongWorkspace := "not-the-bound-workspace"
	mutation.WorkspaceID = &wrongWorkspace
	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("mismatched Workspace recovery error=%v want conflict", err)
	}

	mutation.WorkspaceID = &f.workspace.ID
	wrongIssue := "not-the-bound-issue"
	mutation.IssueID = wrongIssue
	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("mismatched Issue recovery error=%v want conflict", err)
	}

	mutation.IssueID = f.issue.ID
	wrongRun := "00000000-0000-0000-0000-000000000001"
	mutation.RunID = &wrongRun
	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mismatched Run recovery error=%v want not found", err)
	}

	mutation.RunID = &f.run.ID
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='FAILED' WHERE project_id=$1 AND id=$2`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("set terminal fixture Run status: %v", err)
	}
	if _, err := s.SetIssueStatus(ctx, mutation); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("terminal Run recovery error=%v want conflict", err)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 0)
}

func TestRecoveredAgentIssueStatusMutationAllowsEarlierSameRunStatus(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-recovery-same-run")
	agentActor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE runs
		SET status='RUNNING', started_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("start fixture Run: %v", err)
	}

	liveMutation := store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "IN_PROGRESS",
		Actor:       agentActor,
		RunID:       &f.run.ID,
		AgentID:     &f.agent.ID,
		WorkspaceID: &f.workspace.ID,
	}
	if _, err := s.SetIssueStatus(ctx, liveMutation); err != nil {
		t.Fatalf("live same-Run IN_PROGRESS mutation: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	assertIssueStatusEventCount(t, s, f, 1)

	recovered := liveMutation
	recovered.Status = "REVIEW"
	recovered.Recovery = true
	if _, err := s.SetIssueStatus(ctx, recovered); err != nil {
		t.Fatalf("recovered later same-Run REVIEW mutation: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "REVIEW")
	assertIssueStatusEventCount(t, s, f, 2)

	if _, err := s.SetIssueStatus(ctx, recovered); err != nil {
		t.Fatalf("repeated recovered REVIEW mutation: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "REVIEW")
	assertIssueStatusEventCount(t, s, f, 2)

	run, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil {
		t.Fatalf("get active Run: %v", err)
	}
	if run.Status != "RUNNING" {
		t.Fatalf("Run status=%q want RUNNING", run.Status)
	}
}

func TestRecoveredAgentIssueStatusMutationDoesNotOverwriteHumanMutationDuringRun(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-recovery-human-fence")
	agentActor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	humanActor, err := json.Marshal(map[string]string{"type": store.ActorTypeHuman, "id": "human-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE runs
		SET status='RUNNING', started_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("start fixture Run: %v", err)
	}
	if _, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "IN_PROGRESS",
		Actor:       agentActor,
		RunID:       &f.run.ID,
		AgentID:     &f.agent.ID,
		WorkspaceID: &f.workspace.ID,
	}); err != nil {
		t.Fatalf("live same-Run mutation before human change: %v", err)
	}
	if _, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		Status:    "BLOCKED",
		Actor:     humanActor,
	}); err != nil {
		t.Fatalf("human mutation during Run: %v", err)
	}
	beforeEvents := 2
	assertFixtureIssueStatus(t, s, f, "BLOCKED")
	assertIssueStatusEventCount(t, s, f, beforeEvents)

	_, err = s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "REVIEW",
		Actor:       agentActor,
		RunID:       &f.run.ID,
		AgentID:     &f.agent.ID,
		WorkspaceID: &f.workspace.ID,
		Recovery:    true,
	})
	if !errors.Is(err, store.ErrIssueStatusRecoverySuperseded) {
		t.Fatalf("stale human recovery error=%v want superseded", err)
	}
	assertFixtureIssueStatus(t, s, f, "BLOCKED")
	assertIssueStatusEventCount(t, s, f, beforeEvents)
}

func TestRecoveredAgentIssueStatusMutationDoesNotOverwriteOtherRunMutation(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-recovery-other-run-fence")
	currentActor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	projectID := f.project.ID
	otherAgent, err := s.CreateAgent(ctx, store.Agent{
		ProjectID:      &projectID,
		Name:           "agent-other-status-run-" + f.run.ID,
		Engine:         "test",
		ModelProfileID: f.model.ID,
		EngineSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("create other Agent: %v", err)
	}
	otherRun, err := s.CreateRun(ctx, store.Run{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		WorkspaceID: f.workspace.ID,
		AgentID:     &otherAgent.ID,
		Attempt:     2,
	})
	if err != nil {
		t.Fatalf("create other Run: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE runs
		SET status='RUNNING', started_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE project_id=$1 AND id IN ($2,$3)
	`, f.project.ID, f.run.ID, otherRun.ID); err != nil {
		t.Fatalf("start Runs: %v", err)
	}

	if _, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "IN_PROGRESS",
		Actor:       currentActor,
		RunID:       &f.run.ID,
		AgentID:     &f.agent.ID,
		WorkspaceID: &f.workspace.ID,
	}); err != nil {
		t.Fatalf("current Run live mutation: %v", err)
	}
	otherActor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": otherAgent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "BLOCKED",
		Actor:       otherActor,
		RunID:       &otherRun.ID,
		AgentID:     &otherAgent.ID,
		WorkspaceID: &f.workspace.ID,
	}); err != nil {
		t.Fatalf("other Run live mutation: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "BLOCKED")
	assertIssueStatusEventCount(t, s, f, 2)

	_, err = s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "REVIEW",
		Actor:       currentActor,
		RunID:       &f.run.ID,
		AgentID:     &f.agent.ID,
		WorkspaceID: &f.workspace.ID,
		Recovery:    true,
	})
	if !errors.Is(err, store.ErrIssueStatusRecoverySuperseded) {
		t.Fatalf("other-Run recovery error=%v want superseded", err)
	}
	assertFixtureIssueStatus(t, s, f, "BLOCKED")
	assertIssueStatusEventCount(t, s, f, 2)
}
