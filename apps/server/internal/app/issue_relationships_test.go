package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type relationshipStore struct {
	store.ControlPlaneStore
	project       store.Project
	issues        map[string]store.Issue
	relationships []store.IssueRelationship
	createErr     error
	deleteErr     error
}

func (s *relationshipStore) GetProject(_ context.Context, id string) (store.Project, error) {
	if id != s.project.ID {
		return store.Project{}, store.ErrNotFound
	}
	return s.project, nil
}

func (s *relationshipStore) GetIssue(_ context.Context, projectID, issueID string) (store.Issue, error) {
	value, ok := s.issues[issueID]
	if !ok || value.ProjectID != projectID {
		return store.Issue{}, store.ErrNotFound
	}
	return value, nil
}

func (s *relationshipStore) ListIssueRelationships(context.Context, string, string) ([]store.IssueRelationship, error) {
	return s.relationships, nil
}

func (s *relationshipStore) CreateIssueRelationship(_ context.Context, value store.IssueRelationship) (store.IssueRelationship, error) {
	if s.createErr != nil {
		return store.IssueRelationship{}, s.createErr
	}
	value.ID = "relationship"
	return value, nil
}

func (s *relationshipStore) DeleteIssueRelationship(context.Context, string, string, string) error {
	return s.deleteErr
}

func TestIssueRelationshipServiceContract(t *testing.T) {
	ctx := context.Background()
	projectID := "project"
	source := store.Issue{ID: "source", ProjectID: projectID, Title: "Source", Status: "TODO"}
	target := store.Issue{ID: "target", ProjectID: projectID, Title: "Target", Status: "TODO"}
	backend := &relationshipStore{
		project: store.Project{ID: projectID},
		issues:  map[string]store.Issue{source.ID: source, target.ID: target},
		relationships: []store.IssueRelationship{{
			ID: "relationship", ProjectID: projectID, SourceIssueID: source.ID, TargetIssueID: target.ID, Type: "blocks",
		}},
	}
	svc := New(backend)

	listed, err := svc.ListIssueRelationships(ctx, projectID, source.ID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
	created, err := svc.CreateIssueRelationship(ctx, store.IssueRelationship{ProjectID: projectID, SourceIssueID: source.ID, TargetIssueID: target.ID, Type: "depends_on"})
	if err != nil || created.ID != "relationship" {
		t.Fatalf("create=%+v err=%v", created, err)
	}
	if err := svc.DeleteIssueRelationship(ctx, projectID, source.ID, "relationship"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		call func() error
		code string
	}{
		{"self", func() error { _, err := svc.CreateIssueRelationship(ctx, store.IssueRelationship{ProjectID: projectID, SourceIssueID: source.ID, TargetIssueID: source.ID, Type: "related_to"}); return err }, "issue_relationship_self_reference"},
		{"invalid type", func() error { _, err := svc.CreateIssueRelationship(ctx, store.IssueRelationship{ProjectID: projectID, SourceIssueID: source.ID, TargetIssueID: target.ID, Type: "unknown"}); return err }, "invalid_argument"},
		{"missing target", func() error { _, err := svc.CreateIssueRelationship(ctx, store.IssueRelationship{ProjectID: projectID, SourceIssueID: source.ID, TargetIssueID: "missing", Type: "blocks"}); return err }, "issue_relationship_target_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ae, ok := AsError(tc.call())
			if !ok || ae.Code != tc.code {
				t.Fatalf("error=%v code=%q", ae, tc.code)
			}
		})
	}

	backend.createErr = store.ErrConflict
	if _, err := svc.CreateIssueRelationship(ctx, store.IssueRelationship{ProjectID: projectID, SourceIssueID: source.ID, TargetIssueID: target.ID, Type: "blocks"}); err == nil {
		t.Fatal("expected duplicate relationship error")
	} else if ae, ok := AsError(err); !ok || ae.Code != "issue_relationship_exists" || !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate error=%v", err)
	}
	backend.createErr = nil
	backend.deleteErr = store.ErrNotFound
	if err := svc.DeleteIssueRelationship(ctx, projectID, source.ID, "missing"); err == nil {
		t.Fatal("expected missing relationship error")
	} else if ae, ok := AsError(err); !ok || ae.Code != "issue_relationship_not_found" {
		t.Fatalf("delete error=%v", err)
	}
}
