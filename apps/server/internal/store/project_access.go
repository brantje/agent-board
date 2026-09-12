package store

import (
	"context"
	"errors"
)

const (
	ProjectRoleViewer = "viewer"
	ProjectRoleMember = "member"
	ProjectRoleAdmin  = "admin"
)

var ErrLastProjectAdmin = errors.New("store: project requires an active direct admin")

type ProjectUserAccess struct {
	ProjectID string
	UserID    string
	Role      string
}

type ProjectGroupAccess struct {
	ProjectID string
	GroupID   string
	Role      string
}

type ProjectUserAccessView struct {
	Access ProjectUserAccess
	User   User
}

type ProjectGroupAccessView struct {
	Access ProjectGroupAccess
	Group  Group
}

func ValidProjectRole(role string) bool {
	return role == ProjectRoleViewer || role == ProjectRoleMember || role == ProjectRoleAdmin
}

func ProjectRoleRank(role string) int {
	switch role {
	case ProjectRoleViewer:
		return 1
	case ProjectRoleMember:
		return 2
	case ProjectRoleAdmin:
		return 3
	default:
		return 0
	}
}

func ProjectRoleAtLeast(role, minimum string) bool {
	return ProjectRoleRank(role) >= ProjectRoleRank(minimum) && ProjectRoleRank(minimum) > 0
}

// ProjectAccessStore contains only the persistence operations needed for the
// fixed Project access model. It intentionally keeps direct User and Group
// grants concrete rather than introducing a generalized ACL/principal layer.
type ProjectAccessStore interface {
	CreateProjectWithAdmin(context.Context, Project, string) (Project, error)
	ListProjectsForUser(context.Context, string) ([]Project, error)
	EffectiveProjectRole(context.Context, string, string) (string, error)

	ListProjectUserAccess(context.Context, string) ([]ProjectUserAccessView, error)
	UpsertProjectUserAccess(context.Context, ProjectUserAccess) (ProjectUserAccess, error)
	DeleteProjectUserAccess(context.Context, string, string) error

	ListProjectGroupAccess(context.Context, string) ([]ProjectGroupAccessView, error)
	UpsertProjectGroupAccess(context.Context, ProjectGroupAccess) (ProjectGroupAccess, error)
	DeleteProjectGroupAccess(context.Context, string, string) error
}
