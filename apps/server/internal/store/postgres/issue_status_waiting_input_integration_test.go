package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAgentIssueStatusMutationWaitsForAllInteractiveInputButNotResumeBookkeeping(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-waiting-input")
	actor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE runs
		SET status='WAITING_FOR_INPUT', started_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("set fixture Run waiting: %v", err)
	}

	var answeredID, openID string
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO questions (project_id, issue_id, run_id, prompt, kind, blocking, status, answered_at)
		VALUES ($1, $2, $3, 'Answered input', 'TEXT', true, 'ANSWERED', clock_timestamp())
		RETURNING id::text
	`, f.project.ID, f.issue.ID, f.run.ID).Scan(&answeredID); err != nil {
		t.Fatalf("insert answered Question: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO engine_question_bindings (question_id, project_id, run_id, engine, correlation_key, state)
		VALUES ($1, $2, $3, 'opencode', 'status-waiting-input/answered', 'ANSWERED')
	`, answeredID, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("insert answered binding: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO questions (project_id, issue_id, run_id, prompt, kind, blocking, status)
		VALUES ($1, $2, $3, 'Still open input', 'TEXT', true, 'OPEN')
		RETURNING id::text
	`, f.project.ID, f.issue.ID, f.run.ID).Scan(&openID); err != nil {
		t.Fatalf("insert open Question: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO engine_question_bindings (question_id, project_id, run_id, engine, correlation_key, state)
		VALUES ($1, $2, $3, 'opencode', 'status-waiting-input/open', 'OPEN')
	`, openID, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("insert open binding: %v", err)
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
		t.Fatalf("mutation with open interactive input error=%v want conflict", err)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 0)

	if _, err := s.pool.Exec(ctx, `
		UPDATE questions
		SET status='ANSWERED', answered_at=clock_timestamp()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, openID); err != nil {
		t.Fatalf("answer remaining Question: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE engine_question_bindings
		SET state='ANSWERED', updated_at=clock_timestamp()
		WHERE project_id=$1 AND question_id=$2
	`, f.project.ID, openID); err != nil {
		t.Fatalf("answer remaining interactive binding: %v", err)
	}

	if _, err := s.SetIssueStatus(ctx, mutation); err != nil {
		t.Fatalf("mutation after all interactive input answered: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	assertIssueStatusEventCount(t, s, f, 1)

	run, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil {
		t.Fatalf("get waiting Run: %v", err)
	}
	if run.Status != "WAITING_FOR_INPUT" {
		t.Fatalf("Run status=%q want WAITING_FOR_INPUT until Question resolution commits", run.Status)
	}
}
