package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueStatusChangeCompactsSourceAndAppendsDestination(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	project, err := s.CreateProject(ctx, store.Project{
		Name: "board-status-compaction", IssuePrefix: "BSC", RepositoryPath: "/tmp/board-status-compaction",
		DefaultBranch: "main", WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	create := func(title, status string) store.Issue {
		t.Helper()
		issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: title, Status: status})
		if err != nil {
			t.Fatal(err)
		}
		return issue
	}

	first := create("first", "TODO")
	middle := create("middle", "TODO")
	last := create("last", "TODO")
	backlog := create("backlog", "BACKLOG")

	result, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: project.ID, IssueID: middle.ID, Status: "BACKLOG", Actor: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Issue.BoardPosition != 1 {
		t.Fatalf("destination boardPosition=%d want=1", result.Issue.BoardPosition)
	}
	assertBoardOrder(t, s, project.ID, "TODO", []string{first.ID, last.ID})
	assertBoardOrder(t, s, project.ID, "BACKLOG", []string{backlog.ID, middle.ID})
}
