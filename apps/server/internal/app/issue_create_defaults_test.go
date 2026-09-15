package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueCreateDefaultStore struct {
	store.ControlPlaneStore
	created store.Issue
}

func (s *issueCreateDefaultStore) GetProject(_ context.Context, projectID string) (store.Project, error) {
	return store.Project{ID: projectID}, nil
}

func (s *issueCreateDefaultStore) CreateIssueMutation(_ context.Context, input store.Issue) (store.IssueMutationResult, error) {
	s.created = input
	input.ID = "11111111-1111-1111-1111-111111111111"
	input.Key = "AB-1"
	return store.IssueMutationResult{Issue: input}, nil
}

func TestCreateIssueDefaultsMissingStatusToBacklog(t *testing.T) {
	fake := &issueCreateDefaultStore{}
	service := New(fake)

	created, err := service.CreateIssue(t.Context(), store.Issue{ProjectID: "project-1", Title: "Default status"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.created.Status != "BACKLOG" {
		t.Fatalf("persisted status = %q, want BACKLOG", fake.created.Status)
	}
	if created.Status != "BACKLOG" {
		t.Fatalf("returned status = %q, want BACKLOG", created.Status)
	}
}

func TestCreateIssuePreservesExplicitStatus(t *testing.T) {
	fake := &issueCreateDefaultStore{}
	service := New(fake)

	created, err := service.CreateIssue(t.Context(), store.Issue{ProjectID: "project-1", Title: "Explicit status", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.created.Status != "TODO" || created.Status != "TODO" {
		t.Fatalf("explicit status changed: persisted=%q returned=%q", fake.created.Status, created.Status)
	}
}
