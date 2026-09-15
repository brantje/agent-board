package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSetIssueStatusPersistsHumanActorAndSameStatusIsNoOp(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "explicit-human-status")
	actor, err := json.Marshal(map[string]string{"type": store.ActorTypeHuman, "id": "human-1"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: fixture.project.ID,
		IssueID:   fixture.issue.ID,
		Status:    "IN_PROGRESS",
		Actor:     actor,
	})
	if err != nil {
		t.Fatalf("SetIssueStatus() error=%v", err)
	}
	if result.Issue.Status != "IN_PROGRESS" || len(result.Events) != 1 || result.Events[0].Type != "issue.status_changed" {
		t.Fatalf("result=%+v", result)
	}

	var persistedActor []byte
	var runID *string
	if err := s.pool.QueryRow(ctx, `
		SELECT actor, run_id::text
		FROM events
		WHERE project_id=$1 AND issue_id=$2 AND type='issue.status_changed'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, fixture.project.ID, fixture.issue.ID).Scan(&persistedActor, &runID); err != nil {
		t.Fatalf("read status event: %v", err)
	}
	var attributed struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(persistedActor, &attributed); err != nil {
		t.Fatalf("decode actor: %v", err)
	}
	if attributed.Type != store.ActorTypeHuman || attributed.ID != "human-1" || runID != nil {
		t.Fatalf("actor=%+v runID=%v", attributed, runID)
	}

	retry, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: fixture.project.ID,
		IssueID:   fixture.issue.ID,
		Status:    "IN_PROGRESS",
		Actor:     actor,
	})
	if err != nil {
		t.Fatalf("same-status SetIssueStatus() error=%v", err)
	}
	if retry.Issue.Status != "IN_PROGRESS" || len(retry.Events) != 0 {
		t.Fatalf("same-status result=%+v", retry)
	}

	var count int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM events
		WHERE project_id=$1 AND issue_id=$2 AND type='issue.status_changed'
	`, fixture.project.ID, fixture.issue.ID).Scan(&count); err != nil {
		t.Fatalf("count status events: %v", err)
	}
	if count != 1 {
		t.Fatalf("status event count=%d want=1", count)
	}
}
