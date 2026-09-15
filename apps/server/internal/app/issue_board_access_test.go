package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueBoardAccessStore struct {
	*projectAccessServiceStore
	placed *store.IssueBoardPlacement
}

func (s *issueBoardAccessStore) PlaceIssue(_ context.Context, input store.IssueBoardPlacement) (store.IssueMutationResult, error) {
	s.placed = &input
	return store.IssueMutationResult{Issue: store.Issue{
		ID: input.IssueID, ProjectID: input.ProjectID, Title: "issue", Status: input.Status,
	}}, nil
}

func TestProjectAccessBoardPlacementUsesWorkflowMutationRole(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	roles := map[string]string{
		project.ID + ":viewer": store.ProjectRoleViewer,
		project.ID + ":member": store.ProjectRoleMember,
		project.ID + ":admin":  store.ProjectRoleAdmin,
	}

	for _, actorID := range []string{"member", "admin"} {
		t.Run(actorID+" allowed", func(t *testing.T) {
			fake := &issueBoardAccessStore{projectAccessServiceStore: &projectAccessServiceStore{
				projects: []store.Project{project}, roles: roles,
			}}
			service, err := NewProjectAccessService(New(fake), fake)
			if err != nil {
				t.Fatal(err)
			}
			actor := activeProjectActor(actorID, store.DeploymentRoleMember)
			if _, err := service.PlaceIssue(t.Context(), actor, store.IssueBoardPlacement{
				ProjectID: project.ID, IssueID: "issue-1", Status: "TODO",
			}); err != nil {
				t.Fatalf("PlaceIssue() error=%v", err)
			}
			if fake.placed == nil || fake.placed.Actor == nil {
				t.Fatalf("placement was not forwarded with authenticated actor: %+v", fake.placed)
			}
		})
	}

	t.Run("viewer rejected", func(t *testing.T) {
		fake := &issueBoardAccessStore{projectAccessServiceStore: &projectAccessServiceStore{
			projects: []store.Project{project}, roles: roles,
		}}
		service, err := NewProjectAccessService(New(fake), fake)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.PlaceIssue(t.Context(), activeProjectActor("viewer", store.DeploymentRoleMember), store.IssueBoardPlacement{
			ProjectID: project.ID, IssueID: "issue-1", Status: "TODO",
		})
		if appErrorCode(err) != "forbidden" {
			t.Fatalf("viewer PlaceIssue() error=%v", err)
		}
		if fake.placed != nil {
			t.Fatalf("viewer placement reached store: %+v", fake.placed)
		}
	})
}
