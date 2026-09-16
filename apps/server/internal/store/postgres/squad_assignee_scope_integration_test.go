package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadAssigneeDatabaseScopeGuard(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	local := seedRunFixture(t, s, "squad-scope-local")
	foreign := seedRunFixture(t, s, "squad-scope-foreign")

	localSquad, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     local.project.ID,
		Name:          "Local Squad",
		LeaderAgentID: local.agent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignSquad, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     foreign.project.ID,
		Name:          "Foreign Squad",
		LeaderAgentID: foreign.agent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='SQUAD',assignee_id=$2 WHERE id=$1`, local.issue.ID, localSquad.ID); err != nil {
		t.Fatalf("same-project Squad assignment rejected: %v", err)
	}
	issue, err := s.GetIssue(ctx, local.project.ID, local.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	assigned := issue.AssignedTo()
	if assigned == nil || assigned.Type != "SQUAD" || assigned.ID != localSquad.ID || assigned.Name != localSquad.Name {
		t.Fatalf("assigned=%+v", assigned)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='SQUAD',assignee_id=$2 WHERE id=$1`, local.issue.ID, foreignSquad.ID); err == nil {
		t.Fatal("cross-project Squad assignment unexpectedly succeeded")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='SQUAD',assignee_id='00000000-0000-4000-8000-000000000999' WHERE id=$1`, local.issue.ID); err == nil {
		t.Fatal("missing Squad assignment unexpectedly succeeded")
	}
}
