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
