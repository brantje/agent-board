package store

import (
	"context"
	"time"
)

type Group struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GroupStore interface {
	ListGroups(context.Context) ([]Group, error)
	CreateGroup(context.Context, Group) (Group, error)
	UpdateGroup(context.Context, string, string) (Group, error)
	DeleteGroup(context.Context, string) error
	ListGroupMembers(context.Context, string) ([]User, error)
	AddGroupMember(context.Context, string, string) error
	RemoveGroupMember(context.Context, string, string) error
}
