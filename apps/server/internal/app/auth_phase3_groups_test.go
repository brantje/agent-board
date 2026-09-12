package app

import (
	"bytes"
	"context"
	"sort"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type groupAuthMemory struct {
	*authMemory
	groups  map[string]store.Group
	members map[string]map[string]struct{}
	next    int
}

func newGroupAuthMemory() *groupAuthMemory {
	return &groupAuthMemory{
		authMemory: newAuthMemory(),
		groups:     map[string]store.Group{},
		members:    map[string]map[string]struct{}{},
	}
}

func groupAuthTestService(t *testing.T, memory *groupAuthMemory, now *time.Time) *AuthService {
	t.Helper()
	service, err := NewAuthService(memory, AuthServiceConfig{
		Now:        func() time.Time { return *now },
		Random:     &deterministicReader{},
		SigningKey: bytes.Repeat([]byte{9}, 32),
	})
	if err != nil {
		t.Fatalf("new group auth service: %v", err)
	}
	return service
}

func (m *groupAuthMemory) ListGroups(context.Context) ([]store.Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]store.Group, 0, len(m.groups))
	for _, group := range m.groups {
		result = append(result, group)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (m *groupAuthMemory) CreateGroup(_ context.Context, input store.Group) (store.Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.groups {
		if existing.Name == input.Name {
			return store.Group{}, store.ErrConflict
		}
	}
	m.next++
	input.ID = "group-" + input.Name
	input.CreatedAt = time.Unix(int64(m.next), 0).UTC()
	input.UpdatedAt = input.CreatedAt
	m.groups[input.ID] = input
	return input, nil
}

func (m *groupAuthMemory) UpdateGroup(_ context.Context, id, name string) (store.Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	group, ok := m.groups[id]
	if !ok {
		return store.Group{}, store.ErrNotFound
	}
	for otherID, existing := range m.groups {
		if otherID != id && existing.Name == name {
			return store.Group{}, store.ErrConflict
		}
	}
	group.Name = name
	group.UpdatedAt = group.UpdatedAt.Add(time.Second)
	m.groups[id] = group
	return group, nil
}

func (m *groupAuthMemory) DeleteGroup(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.groups, id)
	delete(m.members, id)
	return nil
}

func (m *groupAuthMemory) ListGroupMembers(_ context.Context, groupID string) ([]store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[groupID]; !ok {
		return nil, store.ErrNotFound
	}
	result := make([]store.User, 0, len(m.members[groupID]))
	for userID := range m.members[groupID] {
		result = append(result, m.users[userID])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Username < result[j].Username })
	return result, nil
}

func (m *groupAuthMemory) AddGroupMember(_ context.Context, groupID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[groupID]; !ok {
		return store.ErrNotFound
	}
	if m.members[groupID] == nil {
		m.members[groupID] = map[string]struct{}{}
	}
	if _, exists := m.members[groupID][userID]; exists {
		return store.ErrConflict
	}
	m.members[groupID][userID] = struct{}{}
	return nil
}

func (m *groupAuthMemory) RemoveGroupMember(_ context.Context, groupID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[groupID]; !ok {
		return store.ErrNotFound
	}
	if _, exists := m.members[groupID][userID]; !exists {
		return store.ErrGroupMemberNotFound
	}
	delete(m.members[groupID], userID)
	return nil
}

func TestPhase3GroupAdministrationAndMembership(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	memory := newGroupAuthMemory()
	service := groupAuthTestService(t, memory, &now)
	admin := phase2Admin()
	member := AuthenticatedUser{ID: "member", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}

	if _, err := service.CreateGroup(ctx, member, "engineering"); err == nil {
		t.Fatal("deployment member created a group")
	}
	group, err := service.CreateGroup(ctx, admin, " Engineering ")
	if err != nil {
		t.Fatal(err)
	}
	if group.Name != "engineering" {
		t.Fatalf("group name was not normalized: %+v", group)
	}
	if _, err := service.CreateGroup(ctx, admin, "ENGINEERING"); err == nil {
		t.Fatal("duplicate normalized group name was accepted")
	}
	if _, err := service.CreateGroup(ctx, admin, "   "); err == nil {
		t.Fatal("blank group name was accepted")
	}
	updated, err := service.UpdateGroup(ctx, admin, group.ID, " Platform ")
	if err != nil || updated.Name != "platform" {
		t.Fatalf("update group = %+v, %v", updated, err)
	}
	listed, err := service.ListGroups(ctx, admin)
	if err != nil || len(listed) != 1 || listed[0].ID != group.ID {
		t.Fatalf("list groups = %+v, %v", listed, err)
	}

	memory.users["active"] = store.User{ID: "active", Username: "active", Email: "active@example.com", DisplayName: "Active", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive, AuthVersion: 1}
	memory.users["disabled"] = store.User{ID: "disabled", Username: "disabled", Email: "disabled@example.com", DisplayName: "Disabled", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusDisabled, AuthVersion: 1}
	if err := service.AddGroupMember(ctx, admin, group.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGroupMember(ctx, admin, group.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGroupMember(ctx, admin, group.ID, "active"); err == nil {
		t.Fatal("duplicate membership was accepted")
	}
	if err := service.AddGroupMember(ctx, admin, group.ID, group.ID); err == nil {
		t.Fatal("group identifier was accepted as a group member")
	}
	members, err := service.ListGroupMembers(ctx, admin, group.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("group members = %+v, %v", members, err)
	}
	if members[1].Status != store.UserStatusDisabled {
		t.Fatalf("disabled group member was not retained: %+v", members)
	}
	if memory.users["active"].DeploymentRole != store.DeploymentRoleMember {
		t.Fatal("group membership changed deployment role")
	}
	if err := service.RemoveGroupMember(ctx, admin, group.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveGroupMember(ctx, admin, group.ID, "active"); err == nil {
		t.Fatal("removing absent membership succeeded")
	}

	if err := service.DeleteGroup(ctx, admin, group.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListGroupMembers(ctx, admin, group.ID); err == nil {
		t.Fatal("deleted group still exposed memberships")
	}
	if _, err := memory.GetUser(ctx, "disabled"); err != nil {
		t.Fatalf("deleting group removed user: %v", err)
	}
}

func TestPhase3GroupAdministrationRequiresNormalDeploymentAdmin(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	service := groupAuthTestService(t, newGroupAuthMemory(), &now)
	ctx := context.Background()
	forced := AuthenticatedUser{ID: "admin", DeploymentRole: store.DeploymentRoleAdmin, Status: store.UserStatusActive, ForcePasswordChange: true}

	if _, err := service.ListGroups(ctx, forced); err == nil {
		t.Fatal("forced-password-change admin listed groups")
	}
	if _, err := service.CreateGroup(ctx, forced, "team"); err == nil {
		t.Fatal("forced-password-change admin created group")
	}
}
