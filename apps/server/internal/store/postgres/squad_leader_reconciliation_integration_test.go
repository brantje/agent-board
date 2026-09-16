package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadLeaderChangeReconcilesNewLeaderWithoutRewritingOwnership(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-leader-change")
	ctx := t.Context()
	squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	ownerType := "SQUAD"
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "Leader change", Status: "TODO",
		AssigneeType: &ownerType, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	newLeader, err := s.CreateAgent(ctx, store.Agent{
		ProjectID: &f.project.ID, Name: "replacement-leader", Engine: "scripted",
		ModelProfileID: f.model.ID, EngineSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}

	service := app.New(s)
	squad.LeaderAgentID = newLeader.ID
	updated, err := service.UpdateSquad(ctx, squad)
	if err != nil || updated.LeaderAgentID != newLeader.ID {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	persisted, err := s.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
		t.Fatalf("ownership=%+v err=%v", persisted.AssignedTo(), err)
	}

	rows, err := s.pool.Query(ctx, `SELECT agent_id::text,status FROM runs WHERE issue_id=$1 ORDER BY attempt`, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var agentID, status string
		if err := rows.Scan(&agentID, &status); err != nil {
			t.Fatal(err)
		}
		got[agentID] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[f.agent.ID] != "QUEUED" || got[newLeader.ID] != "QUEUED" {
		t.Fatalf("runs=%+v", got)
	}

	if _, err := service.UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE issue_id=$1`, issue.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate reconciliation count=%d err=%v", count, err)
	}
}
