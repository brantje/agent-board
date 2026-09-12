package app

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const projectDirectoryLimit = 50

type ProjectDirectoryUser struct {
	ID          string
	Username    string
	Email       string
	DisplayName string
}

type ProjectDirectoryGroup struct {
	ID   string
	Name string
}

func (s *Service) projectAccessStore() (store.ProjectAccessStore, error) {
	if s == nil {
		return nil, errors.New("control-plane service is required")
	}
	value, ok := s.store.(store.ProjectAccessStore)
	if !ok {
		return nil, errors.New("project access store is required")
	}
	return value, nil
}

func (s *Service) ListProjectsForActor(ctx context.Context, actor AuthenticatedUser) ([]store.Project, error) {
	if err := requireNormalAuthenticatedUser(actor); err != nil {
		return nil, err
	}
	if actor.DeploymentRole == store.DeploymentRoleAdmin {
		return s.ListProjects(ctx)
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return nil, err
	}
	return accessStore.ListProjectsForUser(ctx, actor.ID)
}

func (s *Service) CreateProjectForActor(ctx context.Context, actor AuthenticatedUser, input store.Project) (store.Project, error) {
	if err := requireNormalAuthenticatedUser(actor); err != nil {
		return store.Project{}, err
	}
	if actor.DeploymentRole != store.DeploymentRoleMember && actor.DeploymentRole != store.DeploymentRoleAdmin {
		return store.Project{}, NewError("forbidden", "project creation is not allowed", store.ErrInvalidArgument)
	}
	if err := validateProject(input); err != nil {
		return store.Project{}, err
	}
	prepared, err := s.ensureProjectRepository(ctx, input)
	if err != nil {
		return store.Project{}, err
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return store.Project{}, err
	}
	project, err := accessStore.CreateProjectWithAdmin(ctx, prepared, actor.ID)
	return project, translateStoreError(err, "project")
}

func (s *Service) EffectiveProjectRole(ctx context.Context, actor AuthenticatedUser, projectID string) (string, error) {
	if err := requireNormalAuthenticatedUser(actor); err != nil {
		return "", err
	}
	if actor.DeploymentRole == store.DeploymentRoleAdmin {
		if _, err := s.GetProject(ctx, projectID); err != nil {
			return "", err
		}
		return store.ProjectRoleAdmin, nil
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return "", err
	}
	role, err := accessStore.EffectiveProjectRole(ctx, projectID, actor.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "", NewError("project_not_found", "project not found", err)
	}
	if err != nil {
		return "", err
	}
	return role, nil
}

func (s *Service) RequireProjectRole(ctx context.Context, actor AuthenticatedUser, projectID, minimumRole string) (string, error) {
	if !store.ValidProjectRole(minimumRole) {
		return "", NewError("invalid_argument", "project role is invalid", store.ErrInvalidArgument)
	}
	role, err := s.EffectiveProjectRole(ctx, actor, projectID)
	if err != nil {
		return "", err
	}
	if !store.ProjectRoleAtLeast(role, minimumRole) {
		return "", NewError("forbidden", "project role does not permit this operation", store.ErrInvalidArgument)
	}
	return role, nil
}

func (s *Service) GetProjectForActor(ctx context.Context, actor AuthenticatedUser, projectID string) (store.Project, string, error) {
	role, err := s.RequireProjectRole(ctx, actor, projectID, store.ProjectRoleViewer)
	if err != nil {
		return store.Project{}, "", err
	}
	project, err := s.GetProject(ctx, projectID)
	return project, role, err
}

func (s *Service) UpdateProjectForActor(ctx context.Context, actor AuthenticatedUser, input store.Project) (store.Project, error) {
	if _, err := s.RequireProjectRole(ctx, actor, input.ID, store.ProjectRoleAdmin); err != nil {
		return store.Project{}, err
	}
	return s.UpdateProject(ctx, input)
}

func (s *Service) ListProjectUserAccessForActor(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.ProjectUserAccessView, error) {
	if _, err := s.RequireProjectRole(ctx, actor, projectID, store.ProjectRoleAdmin); err != nil {
		return nil, err
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return nil, err
	}
	values, err := accessStore.ListProjectUserAccess(ctx, projectID)
	return values, projectAccessStoreError(err)
}

func (s *Service) UpsertProjectUserAccessForActor(ctx context.Context, actor AuthenticatedUser, input store.ProjectUserAccess) (store.ProjectUserAccess, error) {
	if _, err := s.RequireProjectRole(ctx, actor, input.ProjectID, store.ProjectRoleAdmin); err != nil {
		return store.ProjectUserAccess{}, err
	}
	if !store.ValidProjectRole(input.Role) {
		return store.ProjectUserAccess{}, NewError("invalid_argument", "project role must be admin, member, or viewer", store.ErrInvalidArgument)
	}
	authStore, ok := s.store.(store.AuthStore)
	if !ok {
		return store.ProjectUserAccess{}, errors.New("auth store is required")
	}
	if _, err := authStore.GetUser(ctx, input.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ProjectUserAccess{}, NewError("user_not_found", "user not found", err)
		}
		return store.ProjectUserAccess{}, err
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return store.ProjectUserAccess{}, err
	}
	value, err := accessStore.UpsertProjectUserAccess(ctx, input)
	return value, projectAccessStoreError(err)
}

func (s *Service) DeleteProjectUserAccessForActor(ctx context.Context, actor AuthenticatedUser, projectID, userID string) error {
	if _, err := s.RequireProjectRole(ctx, actor, projectID, store.ProjectRoleAdmin); err != nil {
		return err
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return err
	}
	return projectAccessStoreError(accessStore.DeleteProjectUserAccess(ctx, projectID, userID))
}

func (s *Service) ListProjectGroupAccessForActor(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.ProjectGroupAccessView, error) {
	if _, err := s.RequireProjectRole(ctx, actor, projectID, store.ProjectRoleAdmin); err != nil {
		return nil, err
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return nil, err
	}
	values, err := accessStore.ListProjectGroupAccess(ctx, projectID)
	return values, projectAccessStoreError(err)
}

func (s *Service) UpsertProjectGroupAccessForActor(ctx context.Context, actor AuthenticatedUser, input store.ProjectGroupAccess) (store.ProjectGroupAccess, error) {
	if _, err := s.RequireProjectRole(ctx, actor, input.ProjectID, store.ProjectRoleAdmin); err != nil {
		return store.ProjectGroupAccess{}, err
	}
	if !store.ValidProjectRole(input.Role) {
		return store.ProjectGroupAccess{}, NewError("invalid_argument", "project role must be admin, member, or viewer", store.ErrInvalidArgument)
	}
	groupStore, ok := s.store.(store.GroupStore)
	if !ok {
		return store.ProjectGroupAccess{}, errors.New("group store is required")
	}
	groups, err := groupStore.ListGroups(ctx)
	if err != nil {
		return store.ProjectGroupAccess{}, err
	}
	found := false
	for _, group := range groups {
		if group.ID == input.GroupID {
			found = true
			break
		}
	}
	if !found {
		return store.ProjectGroupAccess{}, NewError("group_not_found", "group not found", store.ErrNotFound)
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return store.ProjectGroupAccess{}, err
	}
	value, err := accessStore.UpsertProjectGroupAccess(ctx, input)
	return value, projectAccessStoreError(err)
}

func (s *Service) DeleteProjectGroupAccessForActor(ctx context.Context, actor AuthenticatedUser, projectID, groupID string) error {
	if _, err := s.RequireProjectRole(ctx, actor, projectID, store.ProjectRoleAdmin); err != nil {
		return err
	}
	accessStore, err := s.projectAccessStore()
	if err != nil {
		return err
	}
	return projectAccessStoreError(accessStore.DeleteProjectGroupAccess(ctx, projectID, groupID))
}

func (s *Service) SearchProjectUsersForActor(ctx context.Context, actor AuthenticatedUser, projectID, query string) ([]ProjectDirectoryUser, error) {
	if _, err := s.RequireProjectRole(ctx, actor, projectID, store.ProjectRoleAdmin); err != nil {
		return nil, err
	}
	userStore, ok := s.store.(store.AuthPhase2Store)
	if !ok {
		return nil, errors.New("user directory store is required")
	}
	users, err := userStore.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]ProjectDirectoryUser, 0)
	for _, user := range users {
		if user.Status != store.UserStatusActive {
			continue
		}
		if query != "" && !containsFold(user.Username, query) && !containsFold(user.Email, query) && !containsFold(user.DisplayName, query) {
			continue
		}
		result = append(result, ProjectDirectoryUser{ID: user.ID, Username: user.Username, Email: user.Email, DisplayName: user.DisplayName})
		if len(result) == projectDirectoryLimit {
			break
		}
	}
	return result, nil
}

func (s *Service) SearchProjectGroupsForActor(ctx context.Context, actor AuthenticatedUser, projectID, query string) ([]ProjectDirectoryGroup, error) {
	if _, err := s.RequireProjectRole(ctx, actor, projectID, store.ProjectRoleAdmin); err != nil {
		return nil, err
	}
	groupStore, ok := s.store.(store.GroupStore)
	if !ok {
		return nil, errors.New("group store is required")
	}
	groups, err := groupStore.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]ProjectDirectoryGroup, 0)
	for _, group := range groups {
		if query != "" && !containsFold(group.Name, query) {
			continue
		}
		result = append(result, ProjectDirectoryGroup{ID: group.ID, Name: group.Name})
		if len(result) == projectDirectoryLimit {
			break
		}
	}
	return result, nil
}

func containsFold(value, lowerQuery string) bool {
	return strings.Contains(strings.ToLower(value), lowerQuery)
}

func projectAccessStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrLastProjectAdmin):
		return NewError("last_project_admin", "every project must retain an active direct user admin", err)
	case errors.Is(err, store.ErrNotFound):
		return NewError("project_access_not_found", "project access grant not found", err)
	case errors.Is(err, store.ErrInvalidArgument):
		return NewError("invalid_argument", "invalid project access request", err)
	case errors.Is(err, store.ErrConflict):
		return NewError("conflict", "project access conflicts with existing state", err)
	default:
		return err
	}
}
