package app

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func requireGroupErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected application error %q, got %v", code, err)
	}
	if apiErr.Code != code {
		t.Fatalf("error code = %q, want %q", apiErr.Code, code)
	}
}

func TestGroupApplicationErrorContracts(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	admin := deploymentAdminActor()
	member := AuthenticatedUser{ID: "member", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}

	withoutGroups := authTestService(t, newAuthMemory(), &now)
	_, err := withoutGroups.ListGroups(ctx, admin)
	requireGroupErrorCode(t, err, "group_management_unavailable")

	memory := newGroupAuthMemory()
	service := groupAuthTestService(t, memory, &now)
	group, err := service.CreateGroup(ctx, admin, "engineering")
	if err != nil {
		t.Fatal(err)
	}
	memory.users["active"] = store.User{
		ID:             "active",
		Username:       "active",
		Email:          "active@example.com",
		DisplayName:    "Active",
		DeploymentRole: store.DeploymentRoleMember,
		Status:         store.UserStatusActive,
		AuthVersion:    1,
	}

	if _, err := service.UpdateGroup(ctx, member, group.ID, "platform"); err == nil {
		t.Fatal("deployment member updated a group")
	}
	if err := service.DeleteGroup(ctx, member, group.ID); err == nil {
		t.Fatal("deployment member deleted a group")
	}
	if _, err := service.ListGroupMembers(ctx, member, group.ID); err == nil {
		t.Fatal("deployment member listed group members")
	}
	if err := service.AddGroupMember(ctx, member, group.ID, "active"); err == nil {
		t.Fatal("deployment member added a group member")
	}
	if err := service.RemoveGroupMember(ctx, member, group.ID, "active"); err == nil {
		t.Fatal("deployment member removed a group member")
	}

	_, err = service.UpdateGroup(ctx, admin, "missing", "platform")
	requireGroupErrorCode(t, err, "group_not_found")
	err = service.DeleteGroup(ctx, admin, "missing")
	requireGroupErrorCode(t, err, "group_not_found")
	_, err = service.ListGroupMembers(ctx, admin, "missing")
	requireGroupErrorCode(t, err, "group_not_found")

	err = service.AddGroupMember(ctx, admin, group.ID, "missing-user")
	requireGroupErrorCode(t, err, "user_not_found")
	err = service.AddGroupMember(ctx, admin, "missing", "active")
	requireGroupErrorCode(t, err, "group_not_found")
	err = service.RemoveGroupMember(ctx, admin, "missing", "active")
	requireGroupErrorCode(t, err, "group_not_found")
	err = service.RemoveGroupMember(ctx, admin, group.ID, "active")
	requireGroupErrorCode(t, err, "group_member_not_found")

	if _, err := service.UpdateGroup(ctx, admin, group.ID, "   "); err == nil {
		t.Fatal("blank group rename was accepted")
	}
}
