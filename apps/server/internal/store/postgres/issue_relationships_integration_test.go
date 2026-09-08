package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssuePriorityAndRelationships(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProjectInput("relationships", "/repo/relationships", "REL"))
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := s.CreateProject(ctx, testProjectInput("relationships-other", "/repo/relationships-other", "RELO"))
	if err != nil {
		t.Fatal(err)
	}

	source, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Source", Status: "TODO", Priority: 4})
	if err != nil {
		t.Fatal(err)
	}
	if source.Priority != 4 {
		t.Fatalf("priority=%d want 4", source.Priority)
	}
	target, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Target", Status: "BACKLOG", Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.CreateIssue(ctx, store.Issue{ProjectID: otherProject.ID, Title: "Foreign", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}

	source.Priority = 2
	updated, err := s.UpdateIssue(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Priority != 2 {
		t.Fatalf("updated priority=%d want 2", updated.Priority)
	}
	listed, err := s.ListIssues(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Priority != 2 || listed[1].Priority != 1 {
		t.Fatalf("priorities=%+v", listed)
	}

	if _, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Invalid priority", Status: "TODO", Priority: 5}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid priority error=%v want ErrInvalidArgument", err)
	}

	relationship, err := s.CreateIssueRelationship(ctx, store.IssueRelationship{
		ProjectID:     project.ID,
		SourceIssueID: source.ID,
		TargetIssueID: target.ID,
		Type:          "blocks",
	})
	if err != nil {
		t.Fatal(err)
	}
	values, err := s.ListIssueRelationships(ctx, project.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].ID != relationship.ID || values[0].TargetIssueID != target.ID || values[0].Type != "blocks" {
		t.Fatalf("relationships=%+v", values)
	}

	if _, err := s.CreateIssueRelationship(ctx, store.IssueRelationship{ProjectID: project.ID, SourceIssueID: source.ID, TargetIssueID: target.ID, Type: "blocks"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate error=%v want ErrConflict", err)
	}
	if _, err := s.CreateIssueRelationship(ctx, store.IssueRelationship{ProjectID: project.ID, SourceIssueID: source.ID, TargetIssueID: source.ID, Type: "related_to"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("self error=%v want ErrInvalidArgument", err)
	}
	if _, err := s.CreateIssueRelationship(ctx, store.IssueRelationship{ProjectID: project.ID, SourceIssueID: source.ID, TargetIssueID: foreign.ID, Type: "depends_on"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("cross-project error=%v want ErrInvalidArgument", err)
	}

	if err := s.DeleteIssueRelationship(ctx, project.ID, target.ID, relationship.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong source delete=%v want ErrNotFound", err)
	}
	if err := s.DeleteIssueRelationship(ctx, project.ID, source.ID, relationship.ID); err != nil {
		t.Fatal(err)
	}
	values, err = s.ListIssueRelationships(ctx, project.ID, source.ID)
	if err != nil || len(values) != 0 {
		t.Fatalf("relationships after delete=%+v err=%v", values, err)
	}
}
