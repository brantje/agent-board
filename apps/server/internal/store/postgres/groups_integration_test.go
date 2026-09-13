package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestGroupStoreCRUDMembershipAndDeletion(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	active, err := s.CreateUser(ctx, authUser("active", "active@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := s.CreateUser(ctx, authUser("disabled", "disabled@example.com", store.UserStatusDisabled))
	if err != nil {
		t.Fatal(err)
	}

	group, err := s.CreateGroup(ctx, store.Group{Name: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	if group.ID == "" || group.Name != "backend" {
		t.Fatalf("created group = %+v", group)
	}

	listed, err := s.ListGroups(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != group.ID {
		t.Fatalf("listed groups = %+v, %v", listed, err)
	}

	updated, err := s.UpdateGroup(ctx, group.ID, "platform")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "platform" {
		t.Fatalf("updated group = %+v", updated)
	}

	if err := s.AddGroupMember(ctx, group.ID, active.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, group.ID, disabled.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, group.ID, active.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate membership = %v", err)
	}

	members, err := s.ListGroupMembers(ctx, group.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("group members = %+v, %v", members, err)
	}
	if members[1].Status != store.UserStatusDisabled {
		t.Fatalf("disabled member not preserved: %+v", members)
	}

	if err := s.RemoveGroupMember(ctx, group.ID, active.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGroupMember(ctx, group.ID, active.ID); !errors.Is(err, store.ErrGroupMemberNotFound) {
		t.Fatalf("remove missing membership = %v", err)
	}

	if err := s.DeleteGroup(ctx, group.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListGroupMembers(ctx, group.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("members after group deletion = %v", err)
	}
	if _, err := s.GetUser(ctx, disabled.ID); err != nil {
		t.Fatalf("group deletion removed user: %v", err)
	}
}

func TestGroupStoreConstraints(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	group, err := s.CreateGroup(ctx, store.Group{Name: "team"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup(ctx, store.Group{Name: "team"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate group name = %v", err)
	}
	if _, err := s.CreateGroup(ctx, store.Group{Name: " Team "}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("unnormalized group name = %v", err)
	}
	if _, err := s.CreateGroup(ctx, store.Group{Name: "TEAM"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("uppercase group name = %v", err)
	}
	if _, err := s.UpdateGroup(ctx, "00000000-0000-0000-0000-000000000999", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing group update = %v", err)
	}
	if err := s.DeleteGroup(ctx, "00000000-0000-0000-0000-000000000999"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing group delete = %v", err)
	}

	user, err := s.CreateUser(ctx, authUser("valid-member", "valid-member@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	missingGroupID := "00000000-0000-0000-0000-000000000999"
	if err := s.AddGroupMember(ctx, missingGroupID, user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing group membership = %v", err)
	}
	if err := s.RemoveGroupMember(ctx, missingGroupID, user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("remove from missing group = %v", err)
	}
	missingUserID := "00000000-0000-0000-0000-000000000998"
	if err := s.AddGroupMember(ctx, group.ID, missingUserID); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid user membership = %v", err)
	}
}

func TestGroupMembershipDoesNotChangeDeploymentRole(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	user, err := s.CreateUser(ctx, authUser("member", "member@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	group, err := s.CreateGroup(ctx, store.Group{Name: "admins-by-name-only"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, group.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DeploymentRole != store.DeploymentRoleMember {
		t.Fatalf("group membership changed deployment role to %q", stored.DeploymentRole)
	}
}
