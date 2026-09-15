package postgres

import (
	"context"
	"sync"
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
		ProjectID: project.ID, IssueID: third.ID, Status: "TODO", BeforeIssueID: &before, Actor: store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("PlaceIssue(first) error=%v", err)
	}
	if moved.Issue.BoardPosition != 0 || len(moved.Events) != 1 || moved.Events[0].Type != "issue.updated" {
		t.Fatalf("first placement result=%+v", moved)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{third.ID, first.ID, second.ID})

	before = second.ID
	moved, err = s.PlaceIssue(ctx, store.IssueBoardPlacement{
		ProjectID: project.ID, IssueID: third.ID, Status: "TODO", BeforeIssueID: &before, Actor: store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("PlaceIssue(middle) error=%v", err)
	}
	if moved.Issue.BoardPosition != 1 {
		t.Fatalf("middle boardPosition=%d want=1", moved.Issue.BoardPosition)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{first.ID, third.ID, second.ID})

	moved, err = s.PlaceIssue(ctx, store.IssueBoardPlacement{
		ProjectID: project.ID, IssueID: third.ID, Status: "TODO", Actor: store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("PlaceIssue(last) error=%v", err)
	}
	if moved.Issue.BoardPosition != 2 {
		t.Fatalf("last boardPosition=%d want=2", moved.Issue.BoardPosition)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{first.ID, second.ID, third.ID})

	before = backlog.ID
	moved, err = s.PlaceIssue(ctx, store.IssueBoardPlacement{
		ProjectID: project.ID, IssueID: first.ID, Status: "BACKLOG", BeforeIssueID: &before, Actor: store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("PlaceIssue(cross status) error=%v", err)
	}
	if moved.Issue.Status != "BACKLOG" || moved.Issue.BoardPosition != 0 || len(moved.Events) != 1 || moved.Events[0].Type != "issue.status_changed" {
		t.Fatalf("cross-status result=%+v", moved)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{second.ID, third.ID})
	assertBoardOrder(t, s, project.ID, "BACKLOG", []string{first.ID, backlog.ID})

	appended := create("appended", "TODO")
	if appended.BoardPosition != 2 {
		t.Fatalf("new issue boardPosition=%d want=2", appended.BoardPosition)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{second.ID, third.ID, appended.ID})

	statusResult, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: project.ID, IssueID: second.ID, Status: "BACKLOG", Actor: store.EmptyObject,
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
		Name: "board-invalid-anchor", IssuePrefix: "BA", RepositoryPath: "/tmp/board-invalid-anchor",
		DefaultBranch: "main", WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "issue", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	backlog, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "backlog", Status: "BACKLOG"})
	if err != nil {
		t.Fatal(err)
	}

	self := issue.ID
	if _, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{ProjectID: project.ID, IssueID: issue.ID, Status: "TODO", BeforeIssueID: &self}); err != store.ErrInvalidArgument {
		t.Fatalf("self anchor error=%v want=%v", err, store.ErrInvalidArgument)
	}
	wrongState := backlog.ID
	if _, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{ProjectID: project.ID, IssueID: issue.ID, Status: "TODO", BeforeIssueID: &wrongState}); err != store.ErrInvalidArgument {
		t.Fatalf("wrong-state anchor error=%v want=%v", err, store.ErrInvalidArgument)
	}
	missing := "00000000-0000-0000-0000-000000000001"
	if _, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{ProjectID: project.ID, IssueID: issue.ID, Status: "TODO", BeforeIssueID: &missing}); err != store.ErrInvalidArgument {
		t.Fatalf("missing anchor error=%v want=%v", err, store.ErrInvalidArgument)
	}
	if _, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{ProjectID: project.ID, IssueID: issue.ID, Status: "NOPE"}); err != store.ErrInvalidArgument {
		t.Fatalf("invalid status error=%v want=%v", err, store.ErrInvalidArgument)
	}
}

func TestIssueBoardCrossStatusPlacementUsesSharedMutationAutomation(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "board-shared-mutation")

	backlog, err := s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "backlog assigned", Status: "BACKLOG"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetIssueAssignee(ctx, f.project.ID, backlog.ID, &store.Assignee{Type: "AGENT", ID: f.agent.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	var runsBefore int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE project_id=$1 AND issue_id=$2`, f.project.ID, backlog.ID).Scan(&runsBefore); err != nil {
		t.Fatal(err)
	}
	if runsBefore != 0 {
		t.Fatalf("BACKLOG assignment unexpectedly enqueued %d Runs", runsBefore)
	}

	before := f.issue.ID
	result, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{
		ProjectID: f.project.ID, IssueID: backlog.ID, Status: "TODO", BeforeIssueID: &before, Actor: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.Events[0].Type != "run.created" || result.Events[1].Type != "issue.status_changed" {
		t.Fatalf("placement events=%+v want run.created then issue.status_changed", result.Events)
	}
	var runStatus, jobState string
	if err := s.pool.QueryRow(ctx, `
		SELECT run.status, job.state
		FROM runs AS run
		JOIN scheduler_jobs AS job ON job.project_id=run.project_id AND job.run_id=run.id
		WHERE run.project_id=$1 AND run.issue_id=$2
	`, f.project.ID, backlog.ID).Scan(&runStatus, &jobState); err != nil {
		t.Fatal(err)
	}
	if runStatus != "QUEUED" || jobState != "QUEUED" {
		t.Fatalf("placement automation run=%s job=%s want QUEUED/QUEUED", runStatus, jobState)
	}
}

func TestIssueBoardConcurrentPlacementLastSuccessfulWriteWins(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	project, err := s.CreateProject(ctx, store.Project{
		Name: "board-concurrent", IssuePrefix: "BC", RepositoryPath: "/tmp/board-concurrent",
		DefaultBranch: "main", WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	create := func(title string) store.Issue {
		t.Helper()
		issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: title, Status: "TODO"})
		if err != nil {
			t.Fatal(err)
		}
		return issue
	}
	a, b, c, d := create("a"), create("b"), create("c"), create("d")

	completed := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	placements := []struct {
		name   string
		before string
	}{{name: "front", before: a.ID}, {name: "middle", before: c.ID}}
	for _, placement := range placements {
		placement := placement
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{
				ProjectID: project.ID, IssueID: d.ID, Status: "TODO", BeforeIssueID: &placement.before,
			})
			if err != nil {
				errs <- err
				return
			}
			completed <- placement.name
		}()
	}
	wg.Wait()
	close(errs)
	close(completed)
	for err := range errs {
		t.Fatalf("concurrent placement error=%v", err)
	}
	var order []string
	for name := range completed {
		order = append(order, name)
	}
	if len(order) != 2 {
		t.Fatalf("completed placements=%v want 2", order)
	}
	if order[1] == "front" {
		assertBoardOrder(t, s, project.ID, "TODO", []string{d.ID, a.ID, b.ID, c.ID})
	} else {
		assertBoardOrder(t, s, project.ID, "TODO", []string{a.ID, b.ID, d.ID, c.ID})
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
