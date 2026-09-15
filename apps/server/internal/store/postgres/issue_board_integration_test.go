package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueBoardPlacementOrdersWithinAndAcrossStatuses(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	project, err := s.CreateProject(ctx, store.Project{
		Name:             "board-placement",
		IssuePrefix:      "BP",
		RepositoryPath:   "/tmp/board-placement",
		DefaultBranch:    "main",
		WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("CreateProject() error=%v", err)
	}
	create := func(title, status string) store.Issue {
		t.Helper()
		issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: title, Status: status})
		if err != nil {
			t.Fatalf("CreateIssue(%q) error=%v", title, err)
		}
		return issue
	}

	first := create("first", "TODO")
	second := create("second", "TODO")
	third := create("third", "TODO")
	backlog := create("backlog", "BACKLOG")
	if first.BoardPosition != 0 || second.BoardPosition != 1 || third.BoardPosition != 2 || backlog.BoardPosition != 0 {
		t.Fatalf("initial positions=%d,%d,%d backlog=%d", first.BoardPosition, second.BoardPosition, third.BoardPosition, backlog.BoardPosition)
	}

	before := first.ID
	moved, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{
		ProjectID:     project.ID,
		IssueID:       third.ID,
		Status:        "TODO",
		BeforeIssueID: &before,
		Actor:         store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("PlaceIssue(reorder) error=%v", err)
	}
	if moved.Issue.BoardPosition != 0 || len(moved.Events) != 1 || moved.Events[0].Type != "issue.updated" {
		t.Fatalf("reorder result=%+v", moved)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{third.ID, first.ID, second.ID})

	before = backlog.ID
	moved, err = s.PlaceIssue(ctx, store.IssueBoardPlacement{
		ProjectID:     project.ID,
		IssueID:       first.ID,
		Status:        "BACKLOG",
		BeforeIssueID: &before,
		Actor:         store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("PlaceIssue(cross status) error=%v", err)
	}
	if moved.Issue.Status != "BACKLOG" || moved.Issue.BoardPosition != 0 || len(moved.Events) != 1 || moved.Events[0].Type != "issue.status_changed" {
		t.Fatalf("cross-status result=%+v", moved)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{third.ID, second.ID})
	assertBoardOrder(t, s, project.ID, "BACKLOG", []string{first.ID, backlog.ID})

	appended := create("appended", "TODO")
	if appended.BoardPosition != 2 {
		t.Fatalf("new issue boardPosition=%d want=2", appended.BoardPosition)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{third.ID, second.ID, appended.ID})

	statusResult, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: project.ID,
		IssueID:   second.ID,
		Status:    "BACKLOG",
		Actor:     store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("SetIssueStatus() error=%v", err)
	}
	if statusResult.Issue.BoardPosition != 2 {
		t.Fatalf("status append boardPosition=%d want=2", statusResult.Issue.BoardPosition)
	}
	assertBoardOrder(t, s, project.ID, "BACKLOG", []string{first.ID, backlog.ID, second.ID})
}

func TestIssueBoardPlacementRejectsInvalidAnchor(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	project, err := s.CreateProject(ctx, store.Project{
		Name:             "board-invalid-anchor",
		IssuePrefix:      "BA",
		RepositoryPath:   "/tmp/board-invalid-anchor",
		DefaultBranch:    "main",
		WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "issue", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	missing := "00000000-0000-0000-0000-000000000001"
	if _, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{ProjectID: project.ID, IssueID: issue.ID, Status: "TODO", BeforeIssueID: &missing}); err != store.ErrInvalidArgument {
		t.Fatalf("PlaceIssue() error=%v want=%v", err, store.ErrInvalidArgument)
	}
	if _, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{ProjectID: project.ID, IssueID: issue.ID, Status: "NOPE"}); err != store.ErrInvalidArgument {
		t.Fatalf("invalid status error=%v want=%v", err, store.ErrInvalidArgument)
	}
}

func assertBoardOrder(t *testing.T, s *Store, projectID, status string, want []string) {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), `
		SELECT id::text, board_position
		FROM issues
		WHERE project_id=$1 AND status=$2
		ORDER BY board_position, id
	`, projectID, status)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	var positions []int64
	for rows.Next() {
		var id string
		var position int64
		if err := rows.Scan(&id, &position); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("order=%v positions=%v want=%v", got, positions, want)
	}
	for i := range want {
		if got[i] != want[i] || positions[i] != int64(i) {
			t.Fatalf("order=%v positions=%v want=%v", got, positions, want)
		}
	}
}
