package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadIdentitySurvivesAgentRenameAndIsDistinctFromGroups(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	project, agents := createSquadProjectWithAgents(t, s, "squad-identity", 1)
	if _, err := pool.Exec(ctx, `INSERT INTO groups (name) VALUES ('backend')`); err != nil {
		t.Fatalf("create human group: %v", err)
	}

	squad, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     project.ID,
		Name:          "backend",
		LeaderAgentID: agents[0].ID,
	})
	if err != nil {
		t.Fatalf("create Squad with same name as Group: %v", err)
	}

	renamed := agents[0]
	renamed.Name = "renamed-agent"
	if _, err := s.UpdateAgent(ctx, &project.ID, renamed); err != nil {
		t.Fatalf("rename referenced Agent: %v", err)
	}
	got, err := s.GetSquad(ctx, project.ID, squad.ID)
	if err != nil {
		t.Fatalf("get Squad after Agent rename: %v", err)
	}
	if got.LeaderAgentID != agents[0].ID {
		t.Fatalf("leader id=%q want %q", got.LeaderAgentID, agents[0].ID)
	}

	var groupCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE name = 'backend'`).Scan(&groupCount); err != nil {
		t.Fatalf("count human group: %v", err)
	}
	if groupCount != 1 {
		t.Fatalf("group count=%d want 1", groupCount)
	}
	if err := s.DeleteSquad(ctx, project.ID, squad.ID); err != nil {
		t.Fatalf("delete Squad: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE name = 'backend'`).Scan(&groupCount); err != nil {
		t.Fatalf("count human group after Squad delete: %v", err)
	}
	if groupCount != 1 {
		t.Fatalf("deleting Squad changed Group count to %d", groupCount)
	}
}
