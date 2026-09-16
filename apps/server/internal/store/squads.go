package store

import (
	"context"
	"time"
)

type SquadMember struct {
	AgentID string
	Role    *string
}

type Squad struct {
	ID            string
	ProjectID     string
	Name          string
	LeaderAgentID string
	Members       []SquadMember
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SquadStore interface {
	CreateSquad(context.Context, Squad) (Squad, error)
	GetSquad(context.Context, string, string) (Squad, error)
	ListSquads(context.Context, string) ([]Squad, error)
	UpdateSquad(context.Context, Squad) (Squad, error)
	DeleteSquad(context.Context, string, string) error
}
