package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func testProject(name, repositoryPath, issuePrefix string) store.Project {
	return testProjectInput(name, repositoryPath, issuePrefix)
}

func TestCreateProjectRequiresAndNormalizesIssuePrefix(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProject("Alpha", "/repo/alpha", "ab"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if project.IssuePrefix != "AB" {
		t.Fatalf("issue prefix = %q, want AB", project.IssuePrefix)
	}

	if _, err := s.CreateProject(ctx, testProject("Beta", "/repo/beta", "AB")); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate prefix error = %v, want ErrConflict", err)
	}
	if _, err := s.CreateProject(ctx, testProject("Gamma", "/repo/gamma", "1B")); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid prefix error = %v, want ErrInvalidArgument", err)
	}
}

func TestCreateIssueAllocatesAtomicProjectScopedNumbers(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProject("Keys", "/repo/keys", "AB"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	first, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "First", Status: "TODO"})
	if err != nil {
		t.Fatalf("create first issue: %v", err)
	}
	second, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Second", Status: "TODO"})
	if err != nil {
		t.Fatalf("create second issue: %v", err)
	}
	if first.Key != "AB-1" || second.Key != "AB-2" {
		t.Fatalf("keys = %q and %q, want AB-1 and AB-2", first.Key, second.Key)
	}
	if first.Number != 1 || second.Number != 2 {
		t.Fatalf("numbers = %d and %d, want 1 and 2", first.Number, second.Number)
	}

	var wg sync.WaitGroup
	keys := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Concurrent", Status: "TODO"})
			if err != nil {
				t.Errorf("concurrent create: %v", err)
				return
			}
			keys <- issue.Key
		}()
	}
	wg.Wait()
	close(keys)

	seen := map[string]bool{}
	for key := range keys {
		if seen[key] {
			t.Fatalf("duplicate allocated key %q", key)
		}
		seen[key] = true
	}
	if len(seen) != 8 {
		t.Fatalf("allocated %d concurrent keys, want 8", len(seen))
	}
}

func TestGetIssueUUIDByKeyRequiresMatchingProjectPrefix(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProject("Scoped", "/repo/scoped", "AB"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	other, err := s.CreateProject(ctx, testProject("Other", "/repo/other", "XY"))
	if err != nil {
		t.Fatalf("create other project: %v", err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Scoped", Status: "TODO"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	uuid, err := s.GetIssueUUIDByKey(ctx, project.ID, "AB-1")
	if err != nil || uuid != issue.ID {
		t.Fatalf("resolve key in project: uuid=%q err=%v", uuid, err)
	}
	if _, err := s.GetIssueUUIDByKey(ctx, project.ID, "XY-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign prefix error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetIssueUUIDByKey(ctx, other.ID, "AB-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project prefix error = %v, want ErrNotFound", err)
	}
}

func TestUpdateProjectDoesNotChangeIssuePrefix(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProject("Immutable", "/repo/immutable", "AB"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	project.Name = "Renamed"
	project.IssuePrefix = "XY"
	updated, err := s.UpdateProject(ctx, project)
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if updated.IssuePrefix != "AB" {
		t.Fatalf("updated prefix = %q, want AB", updated.IssuePrefix)
	}
}

func TestListIssuesReturnsDerivedKeys(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProject("Board", "/repo/board", "AB"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "One", Status: "TODO"}); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	issues, err := s.ListIssues(ctx, project.ID)
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if len(issues) != 1 || issues[0].Key != "AB-1" {
		t.Fatalf("listed issues = %+v, want AB-1", issues)
	}
}
