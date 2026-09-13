package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectAccessCreationDiscoveryAndHighestRole(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	creator, err := s.CreateUser(ctx, authUser("creator", "creator@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := s.CreateUser(ctx, authUser("unrelated", "unrelated@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Private", "/repo/private", "PRV"), creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.EffectiveProjectRole(ctx, project.ID, creator.ID)
	if err != nil || role != store.ProjectRoleAdmin {
		t.Fatalf("creator role=%q err=%v", role, err)
	}
	projects, err := s.ListProjectsForUser(ctx, unrelated.ID)
	if err != nil || len(projects) != 0 {
		t.Fatalf("unrelated projects=%+v err=%v", projects, err)
	}

	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: unrelated.ID, Role: store.ProjectRoleViewer}); err != nil {
		t.Fatal(err)
	}
	group, err := s.CreateGroup(ctx, store.Group{Name: "workers"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, group.ID, unrelated.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectGroupAccess(ctx, store.ProjectGroupAccess{ProjectID: project.ID, GroupID: group.ID, Role: store.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}
	role, err = s.EffectiveProjectRole(ctx, project.ID, unrelated.ID)
	if err != nil || role != store.ProjectRoleMember {
		t.Fatalf("direct viewer + group member role=%q err=%v", role, err)
	}
	second, err := s.CreateGroup(ctx, store.Group{Name: "admins"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, second.ID, unrelated.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectGroupAccess(ctx, store.ProjectGroupAccess{ProjectID: project.ID, GroupID: second.ID, Role: store.ProjectRoleAdmin}); err != nil {
		t.Fatal(err)
	}
	role, err = s.EffectiveProjectRole(ctx, project.ID, unrelated.ID)
	if err != nil || role != store.ProjectRoleAdmin {
		t.Fatalf("multiple group role=%q err=%v", role, err)
	}

	if err := s.DeleteProjectUserAccess(ctx, project.ID, unrelated.ID); err != nil {
		t.Fatal(err)
	}
	role, err = s.EffectiveProjectRole(ctx, project.ID, unrelated.ID)
	if err != nil || role != store.ProjectRoleAdmin {
		t.Fatalf("inherited role after direct removal=%q err=%v", role, err)
	}
	if err := s.DeleteGroup(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	role, err = s.EffectiveProjectRole(ctx, project.ID, unrelated.ID)
	if err != nil || role != store.ProjectRoleMember {
		t.Fatalf("remaining inherited role after group deletion=%q err=%v", role, err)
	}
}

func TestProjectAccessProtectsLastActiveDirectAdmin(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	creator, err := s.CreateUser(ctx, authUser("creator-admin", "creator-admin@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, authUser("other-admin", "other-admin@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Invariant", "/repo/invariant", "INV"), creator.ID)
	if err != nil {
		t.Fatal(err)
	}

	group, err := s.CreateGroup(ctx, store.Group{Name: "project-admins"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, group.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectGroupAccess(ctx, store.ProjectGroupAccess{ProjectID: project.ID, GroupID: group.ID, Role: store.ProjectRoleAdmin}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProjectUserAccess(ctx, project.ID, creator.ID); !errors.Is(err, store.ErrLastProjectAdmin) {
		t.Fatalf("group admin incorrectly satisfied invariant: %v", err)
	}
	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: creator.ID, Role: store.ProjectRoleMember}); !errors.Is(err, store.ErrLastProjectAdmin) {
		t.Fatalf("demoting final direct admin: %v", err)
	}
	if _, err := s.SetUserDisabled(ctx, creator.ID, true); !errors.Is(err, store.ErrLastProjectAdmin) {
		t.Fatalf("disabling final direct admin: %v", err)
	}

	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: other.ID, Role: store.ProjectRoleAdmin}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProjectUserAccess(ctx, project.ID, creator.ID); err != nil {
		t.Fatalf("remove original after replacement admin: %v", err)
	}
	grants, err := s.ListProjectUserAccess(ctx, project.ID)
	if err != nil || len(grants) != 1 || grants[0].Access.UserID != other.ID || grants[0].Access.Role != store.ProjectRoleAdmin {
		t.Fatalf("direct grants=%+v err=%v", grants, err)
	}
}

func TestConcurrentProjectAdminMutationsCannotRemoveAllAdmins(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	first, err := s.CreateUser(ctx, authUser("first-project-admin", "first-project-admin@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateUser(ctx, authUser("second-project-admin", "second-project-admin@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Concurrent Admin", "/repo/concurrent-admin", "CAD"), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: second.ID, Role: store.ProjectRoleAdmin}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, userID := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			<-start
			errs <- s.DeleteProjectUserAccess(ctx, project.ID, userID)
		}(userID)
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	blocked := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrLastProjectAdmin):
			blocked++
		default:
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if successes != 1 || blocked != 1 {
		t.Fatalf("concurrent results successes=%d blocked=%d", successes, blocked)
	}
	grants, err := s.ListProjectUserAccess(ctx, project.ID)
	if err != nil || len(grants) != 1 || grants[0].Access.Role != store.ProjectRoleAdmin {
		t.Fatalf("remaining grants=%+v err=%v", grants, err)
	}
}

func TestProjectAccessListsDisabledAttachedUserSafely(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	creator, err := s.CreateUser(ctx, authUser("list-owner", "list-owner@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := s.CreateUser(ctx, authUser("attached-disabled", "attached-disabled@example.com", store.UserStatusDisabled))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Disabled Listing", "/repo/disabled-listing", "DIS"), creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: disabled.ID, Role: store.ProjectRoleViewer}); err != nil {
		t.Fatal(err)
	}
	grants, err := s.ListProjectUserAccess(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, grant := range grants {
		if grant.Access.UserID == disabled.ID {
			found = true
			if grant.User.Status != store.UserStatusDisabled || grant.User.Username != disabled.Username {
				t.Fatalf("disabled grant=%+v", grant)
			}
		}
	}
	if !found {
		t.Fatal("disabled attached user missing from access list")
	}
}
