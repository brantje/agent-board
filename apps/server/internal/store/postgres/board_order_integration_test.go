package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func boardIDPtr(value string) *string { return &value }

func TestIssueCreationPlacementAndCanonicalReadOrder(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	bottomProject, err := s.CreateProject(ctx, testProjectInput("bottom-order", "/repo/bottom-order", "BOT"))
	if err != nil {
		t.Fatalf("create bottom project: %v", err)
	}
	bottomFirst, err := s.CreateIssue(ctx, store.Issue{ProjectID: bottomProject.ID, Title: "first", Status: "TODO"})
	if err != nil {
		t.Fatalf("create first bottom issue: %v", err)
	}
	bottomSecond, err := s.CreateIssue(ctx, store.Issue{ProjectID: bottomProject.ID, Title: "second", Status: "TODO"})
	if err != nil {
		t.Fatalf("create second bottom issue: %v", err)
	}
	assertBoardOrder(t, s, ctx, bottomProject.ID, "TODO", []string{bottomFirst.ID, bottomSecond.ID})
	assertBoardPositions(t, pool, ctx, bottomProject.ID, map[string]int64{bottomFirst.ID: 0, bottomSecond.ID: 1})

	topInput := testProjectInput("top-order", "/repo/top-order", "TOP")
	topInput.WorkflowSettings = json.RawMessage(`{"newIssuePlacement":"top"}`)
	topProject, err := s.CreateProject(ctx, topInput)
	if err != nil {
		t.Fatalf("create top project: %v", err)
	}
	topFirst, err := s.CreateIssue(ctx, store.Issue{ProjectID: topProject.ID, Title: "first", Status: "TODO"})
	if err != nil {
		t.Fatalf("create first top issue: %v", err)
	}
	topSecond, err := s.CreateIssue(ctx, store.Issue{ProjectID: topProject.ID, Title: "second", Status: "TODO"})
	if err != nil {
		t.Fatalf("create second top issue: %v", err)
	}
	assertBoardOrder(t, s, ctx, topProject.ID, "TODO", []string{topSecond.ID, topFirst.ID})
	assertBoardPositions(t, pool, ctx, topProject.ID, map[string]int64{topSecond.ID: 0, topFirst.ID: 1})
}

func TestPlaceIssueReordersAndMovesExactlyByAnchors(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput("placement", "/repo/placement", "PLC"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	a, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "a", Status: "TODO"})
	b, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "b", Status: "TODO"})
	c, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "c", Status: "TODO"})
	reviewA, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "review-a", Status: "REVIEW"})
	reviewB, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "review-b", Status: "REVIEW"})

	result, err := s.PlaceIssue(ctx, store.IssuePlacement{
		ProjectID: project.ID,
		IssueID:   c.ID,
		BeforeID:  nil,
		AfterID:   boardIDPtr(a.ID),
	}, store.EmptyObject)
	if err != nil {
		t.Fatalf("reorder to top: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != "issue.updated" {
		t.Fatalf("same-state reorder events = %+v, want issue.updated", result.Events)
	}
	assertBoardOrder(t, s, ctx, project.ID, "TODO", []string{c.ID, a.ID, b.ID})

	destination := "REVIEW"
	result, err = s.PlaceIssue(ctx, store.IssuePlacement{
		ProjectID: project.ID,
		IssueID:   a.ID,
		Status:    &destination,
		BeforeID:  boardIDPtr(reviewA.ID),
		AfterID:   boardIDPtr(reviewB.ID),
	}, store.EmptyObject)
	if err != nil {
		t.Fatalf("cross-state placement: %v", err)
	}
	if len(result.Events) == 0 || result.Events[len(result.Events)-1].Type != "issue.status_changed" {
		t.Fatalf("cross-state events = %+v, want status event", result.Events)
	}
	assertBoardOrder(t, s, ctx, project.ID, "TODO", []string{c.ID, b.ID})
	assertBoardOrder(t, s, ctx, project.ID, "REVIEW", []string{reviewA.ID, a.ID, reviewB.ID})

	_, err = s.PlaceIssue(ctx, store.IssuePlacement{
		ProjectID: project.ID,
		IssueID:   b.ID,
		BeforeID:  boardIDPtr(c.ID),
		AfterID:   nil,
	}, store.EmptyObject)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale/non-bottom anchor error = %v, want ErrConflict", err)
	}
	assertBoardOrder(t, s, ctx, project.ID, "TODO", []string{c.ID, b.ID})

	_, err = s.PlaceIssue(ctx, store.IssuePlacement{ProjectID: project.ID, IssueID: c.ID, AfterID: boardIDPtr(c.ID)}, store.EmptyObject)
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("self anchor error = %v, want ErrInvalidArgument", err)
	}
}

func TestNormalStatusChangesPlaceAtDestinationTop(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput("status-top", "/repo/status-top", "STP"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	reviewA, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "review-a", Status: "REVIEW"})
	reviewB, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "review-b", Status: "REVIEW"})
	patchIssue, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "patch", Status: "TODO"})
	statusIssue, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "status", Status: "TODO"})

	review := "REVIEW"
	if _, err := s.UpdateIssuePatchMutation(ctx, store.IssuePatch{ProjectID: project.ID, ID: patchIssue.ID, Status: &review}, store.EmptyObject); err != nil {
		t.Fatalf("patch status: %v", err)
	}
	assertBoardOrder(t, s, ctx, project.ID, "REVIEW", []string{patchIssue.ID, reviewA.ID, reviewB.ID})

	if _, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{ProjectID: project.ID, IssueID: statusIssue.ID, Status: "REVIEW", Actor: store.EmptyObject}); err != nil {
		t.Fatalf("set status: %v", err)
	}
	assertBoardOrder(t, s, ctx, project.ID, "REVIEW", []string{statusIssue.ID, patchIssue.ID, reviewA.ID, reviewB.ID})
}

func assertBoardOrder(t *testing.T, s *Store, ctx context.Context, projectID, status string, want []string) {
	t.Helper()
	issues, err := s.ListIssues(ctx, projectID)
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	got := make([]string, 0, len(want))
	for _, issue := range issues {
		if issue.Status == status {
			got = append(got, issue.ID)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("%s issue count = %d, want %d (%v)", status, len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s order[%d] = %q, want %q (all=%v)", status, index, got[index], want[index], got)
		}
	}
}

func assertBoardPositions(t *testing.T, pool *pgxpool.Pool, ctx context.Context, projectID string, want map[string]int64) {
	t.Helper()
	for issueID, wantPosition := range want {
		var got int64
		if err := pool.QueryRow(ctx, `SELECT board_position FROM issues WHERE project_id=$1 AND id=$2`, projectID, issueID).Scan(&got); err != nil {
			t.Fatalf("read board position for %s: %v", issueID, err)
		}
		if got != wantPosition {
			t.Fatalf("board position for %s = %d, want %d", issueID, got, wantPosition)
		}
	}
}
