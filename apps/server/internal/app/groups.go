package app

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func normalizeGroupName(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", NewError("invalid_group_name", "group name is required", store.ErrInvalidArgument)
	}
	return normalized, nil
}

func (s *AuthService) groupStore() (store.GroupStore, error) {
	groups, ok := s.store.(store.GroupStore)
	if !ok {
		return nil, NewError("group_management_unavailable", "group management is unavailable", store.ErrInvalidArgument)
	}
	return groups, nil
}

func groupStoreError(err error, conflictMessage string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return NewError("group_not_found", "group not found", err)
	case errors.Is(err, store.ErrConflict):
		return NewError("conflict", conflictMessage, err)
	default:
		return err
	}
}

func (s *AuthService) ListGroups(ctx context.Context, actor AuthenticatedUser) ([]store.Group, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return nil, err
	}
	groups, err := s.groupStore()
	if err != nil {
		return nil, err
	}
	return groups.ListGroups(ctx)
}

func (s *AuthService) CreateGroup(ctx context.Context, actor AuthenticatedUser, name string) (store.Group, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return store.Group{}, err
	}
	normalized, err := normalizeGroupName(name)
	if err != nil {
		return store.Group{}, err
	}
	groups, err := s.groupStore()
	if err != nil {
		return store.Group{}, err
	}
	group, err := groups.CreateGroup(ctx, store.Group{Name: normalized})
	if err != nil {
		return store.Group{}, groupStoreError(err, "group name is already in use")
	}
	return group, nil
}

func (s *AuthService) UpdateGroup(ctx context.Context, actor AuthenticatedUser, groupID, name string) (store.Group, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return store.Group{}, err
	}
	normalized, err := normalizeGroupName(name)
	if err != nil {
		return store.Group{}, err
	}
	groups, err := s.groupStore()
	if err != nil {
		return store.Group{}, err
	}
	group, err := groups.UpdateGroup(ctx, groupID, normalized)
	if err != nil {
		return store.Group{}, groupStoreError(err, "group name is already in use")
	}
	return group, nil
}

func (s *AuthService) DeleteGroup(ctx context.Context, actor AuthenticatedUser, groupID string) error {
	if err := requireDeploymentAdmin(actor); err != nil {
		return err
	}
	groups, err := s.groupStore()
	if err != nil {
		return err
	}
	return groupStoreError(groups.DeleteGroup(ctx, groupID), "group conflict")
}

func (s *AuthService) ListGroupMembers(ctx context.Context, actor AuthenticatedUser, groupID string) ([]AuthenticatedUser, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return nil, err
	}
	groups, err := s.groupStore()
	if err != nil {
		return nil, err
	}
	users, err := groups.ListGroupMembers(ctx, groupID)
	if err != nil {
		return nil, groupStoreError(err, "group membership conflict")
	}
	result := make([]AuthenticatedUser, 0, len(users))
	for _, user := range users {
		result = append(result, publicUser(user))
	}
	return result, nil
}

func (s *AuthService) AddGroupMember(ctx context.Context, actor AuthenticatedUser, groupID, userID string) error {
	if err := requireDeploymentAdmin(actor); err != nil {
		return err
	}
	if _, err := s.store.GetUser(ctx, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NewError("user_not_found", "user not found", err)
		}
		return err
	}
	groups, err := s.groupStore()
	if err != nil {
		return err
	}
	if err := groups.AddGroupMember(ctx, groupID, userID); err != nil {
		return groupStoreError(err, "user is already a group member")
	}
	return nil
}

func (s *AuthService) RemoveGroupMember(ctx context.Context, actor AuthenticatedUser, groupID, userID string) error {
	if err := requireDeploymentAdmin(actor); err != nil {
		return err
	}
	groups, err := s.groupStore()
	if err != nil {
		return err
	}
	if err := groups.RemoveGroupMember(ctx, groupID, userID); err != nil {
		if errors.Is(err, store.ErrGroupMemberNotFound) {
			return NewError("group_member_not_found", "group member not found", err)
		}
		return groupStoreError(err, "group membership conflict")
	}
	return nil
}
