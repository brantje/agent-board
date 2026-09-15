package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestListProjectMembersReturnsEffectiveActiveHumanAccess(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	creator, err := s.CreateUser(ctx, authUser("member-owner", "member-owner@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	inherited, err := s.CreateUser(ctx, authUser("member-inherited", "member-inherited@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := s.CreateUser(ctx, authUser("member-outsider", "member-outsider@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := s.CreateUser(ctx, authUser("member-disabled", "member-disabled@example.com", store.UserStatusDisabled))
	if err != nil {
		t.Fatal(err)
	}
	deploymentAdminInput := authUser("member-deployment-admin", "member-deployment-admin@example.com", store.UserStatusActive)
	deploymentAdminInput.DeploymentRole = store.DeploymentRoleAdmin
	deploymentAdmin, err := s.CreateUser(ctx, deploymentAdminInput)
	if err != nil {
		t.Fatal(err)
	}

	project, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Member Directory", "/repo/member-directory", "MDR"), creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: inherited.ID, Role: store.ProjectRoleViewer}); err != nil {
		t.Fatal(err)
	}
	group, err := s.CreateGroup(ctx, store.Group{Name: "member-directory-group"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, group.ID, inherited.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectGroupAccess(ctx, store.ProjectGroupAccess{ProjectID: project.ID, GroupID: group.ID, Role: store.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: disabled.ID, Role: store.ProjectRoleViewer}); err != nil {
		t.Fatal(err)
	}

	members, err := s.ListProjectMembers(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]store.ProjectMember, len(members))
	for _, member := range members {
		if _, exists := byID[member.UserID]; exists {
			t.Fatalf("duplicate member %s in %+v", member.UserID, members)
		}
		byID[member.UserID] = member
	}

	if got := byID[creator.ID]; got.Role != store.ProjectRoleAdmin || got.Username != creator.Username || got.DisplayName != creator.DisplayName {
		t.Fatalf("creator member = %+v", got)
	}
	if got := byID[inherited.ID]; got.Role != store.ProjectRoleMember {
		t.Fatalf("inherited member = %+v", got)
	}
	if got := byID[deploymentAdmin.ID]; got.Role != store.ProjectRoleAdmin {
		t.Fatalf("deployment admin member = %+v", got)
	}
	if _, ok := byID[outsider.ID]; ok {
		t.Fatalf("outsider unexpectedly listed: %+v", byID[outsider.ID])
	}
	if _, ok := byID[disabled.ID]; ok {
		t.Fatalf("disabled user unexpectedly listed: %+v", byID[disabled.ID])
	}
}
