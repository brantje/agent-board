package postgres

import (
	"context"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestConcurrentPlacementsSerializeWithoutCorruptingCanonicalOrder(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput("concurrent-placement", "/repo/concurrent-placement", "CNP"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	a, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "a", Status: "TODO"})
	b, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "b", Status: "TODO"})
	c, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "c", Status: "TODO"})

	placements := []store.IssuePlacement{
		{ProjectID: project.ID, IssueID: c.ID, BeforeID: nil, AfterID: boardIDPtr(a.ID)},
		{ProjectID: project.ID, IssueID: c.ID, BeforeID: boardIDPtr(b.ID), AfterID: nil},
	}
	errs := make(chan error, len(placements))
	var wg sync.WaitGroup
	for _, placement := range placements {
		placement := placement
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.PlaceIssue(ctx, placement, store.EmptyObject)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent placement: %v", err)
		}
	}

	issues, err := s.ListIssues(ctx, project.ID)
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	ordered := make([]string, 0, 3)
	for _, issue := range issues {
		if issue.Status == "TODO" {
			ordered = append(ordered, issue.ID)
		}
	}
	if len(ordered) != 3 {
		t.Fatalf("TODO order=%v, want 3 issues", ordered)
	}
	isTopResult := ordered[0] == c.ID && ordered[1] == a.ID && ordered[2] == b.ID
	isBottomResult := ordered[0] == a.ID && ordered[1] == b.ID && ordered[2] == c.ID
	if !isTopResult && !isBottomResult {
		t.Fatalf("concurrent final order=%v, want one complete successful placement", ordered)
	}

	var rows, distinctPositions int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT board_position)
		FROM issues
		WHERE project_id=$1 AND status='TODO'
	`, project.ID).Scan(&rows, &distinctPositions); err != nil {
		t.Fatalf("inspect board positions: %v", err)
	}
	if rows != 3 || distinctPositions != 3 {
		t.Fatalf("board positions rows=%d distinct=%d, want 3/3", rows, distinctPositions)
	}
}

func TestBoardReorderDoesNotCreateSchedulerWork(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput("board-scheduler-independent", "/repo/board-scheduler-independent", "BSI"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	a, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "a", Status: "TODO"})
	b, _ := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "b", Status: "TODO"})

	if _, err := s.PlaceIssue(ctx, store.IssuePlacement{ProjectID: project.ID, IssueID: b.ID, BeforeID: nil, AfterID: boardIDPtr(a.ID)}, store.EmptyObject); err != nil {
		t.Fatalf("reorder issue: %v", err)
	}

	var runs, jobs int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM runs WHERE project_id=$1`, project.ID).Scan(&runs); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM scheduler_jobs WHERE project_id=$1`, project.ID).Scan(&jobs); err != nil {
		t.Fatalf("count scheduler jobs: %v", err)
	}
	if runs != 0 || jobs != 0 {
		t.Fatalf("board reorder created execution work: runs=%d jobs=%d", runs, jobs)
	}
}
