package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectAccessGroupGrantAndDirectoryReads(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	creator, err := s.CreateUser(ctx, authUser("postgres-access-owner", "postgres-access-owner@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateUser(ctx, authUser("postgres-access-member", "postgres-access-member@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := s.CreateUser(ctx, authUser("postgres-access-disabled", "postgres-access-disabled@example.com", store.UserStatusDisabled))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Postgres Access Reads", "/repo/postgres-access-reads", "PAR"), creator.ID)
	if err != nil {
		t.Fatal(err)
	}

	group, err := s.CreateGroup(ctx, store.Group{Name: "postgres-access-team"})
	if err != nil {
		t.Fatal(err)
	}
	otherGroup, err := s.CreateGroup(ctx, store.Group{Name: "postgres-access-other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, group.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: member.ID, Role: store.ProjectRoleViewer}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectGroupAccess(ctx, store.ProjectGroupAccess{ProjectID: project.ID, GroupID: group.ID, Role: store.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}

	grants, err := s.ListProjectGroupAccess(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Access.ProjectID != project.ID || grants[0].Access.GroupID != group.ID || grants[0].Access.Role != store.ProjectRoleMember || grants[0].Group.Name != group.Name {
		t.Fatalf("group grants = %+v", grants)
	}
	if _, err := s.ListProjectGroupAccess(ctx, "00000000-0000-0000-0000-000000000999"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project group list error = %v", err)
	}

	attached, err := s.GetProjectAccessUser(ctx, member.ID)
	if err != nil || attached.ID != member.ID || attached.Username != member.Username {
		t.Fatalf("GetProjectAccessUser() = %+v, %v", attached, err)
	}
	if _, err := s.GetProjectAccessUser(ctx, "00000000-0000-0000-0000-000000000998"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing access user error = %v", err)
	}

	users, err := s.ListProjectAccessUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, user := range users {
		seen[user.ID] = user.Status
	}
	if seen[creator.ID] != store.UserStatusActive || seen[member.ID] != store.UserStatusActive || seen[disabled.ID] != store.UserStatusDisabled {
		t.Fatalf("access directory users = %+v", users)
	}

	groups, err := s.ListProjectAccessGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seenGroups := map[string]string{}
	for _, value := range groups {
		seenGroups[value.ID] = value.Name
	}
	if seenGroups[group.ID] != group.Name || seenGroups[otherGroup.ID] != otherGroup.Name {
		t.Fatalf("access directory groups = %+v", groups)
	}

	if err := s.DeleteProjectGroupAccess(ctx, project.ID, group.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProjectGroupAccess(ctx, project.ID, group.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second group grant delete error = %v", err)
	}
	remaining, err := s.ListProjectGroupAccess(ctx, project.ID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining group grants = %+v, %v", remaining, err)
	}
	role, err := s.EffectiveProjectRole(ctx, project.ID, member.ID)
	if err != nil || role != store.ProjectRoleViewer {
		t.Fatalf("effective role after group grant delete = %q, %v", role, err)
	}
}
