package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *projectAccessServiceStore) ListProjectMembers(_ context.Context, projectID string) ([]store.ProjectMember, error) {
	members := make([]store.ProjectMember, 0)
	for _, user := range s.users {
		if user.Status != store.UserStatusActive {
			continue
		}
		role := s.roles[projectID+":"+user.ID]
		if user.DeploymentRole == store.DeploymentRoleAdmin {
			role = store.ProjectRoleAdmin
		}
		if role == "" {
			continue
		}
		members = append(members, store.ProjectMember{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Role: role})
	}
	return members, nil
}

func TestProjectAccessListProjectMembersUsesViewerAuthorization(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project},
		roles: map[string]string{
			project.ID + ":viewer": store.ProjectRoleViewer,
			project.ID + ":member": store.ProjectRoleMember,
		},
		users: []store.User{{ID: "member", Username: "alice", DisplayName: "Alice", Status: store.UserStatusActive}},
	}
	service := newProjectAccessServiceForTest(t, fake)

	members, err := service.ListProjectMembers(t.Context(), activeProjectActor("viewer", store.DeploymentRoleMember), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].UserID != "member" || members[0].Role != store.ProjectRoleMember {
		t.Fatalf("members = %+v", members)
	}
	if _, err := service.ListProjectMembers(t.Context(), activeProjectActor("outsider", store.DeploymentRoleMember), project.ID); errorCode(err) != "project_not_found" {
		t.Fatalf("outsider error = %v", err)
	}
}
