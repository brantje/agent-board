package store

import (
	"context"
	"time"
)

// Squad is durable Project-scoped Agent team configuration. LeaderAgentID is
// the canonical leadership reference; Members contains only additional Agents.
type Squad struct {
	ID            string
	ProjectID     string
	Name          string
	LeaderAgentID string
	Members       []SquadMember
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SquadMember struct {
	AgentID string
	Role    *string
}

type SquadUpdateResult struct {
	Squad         Squad
	LeaderChanged bool
	Events        []Event
}

type SquadStore interface {
	CreateSquad(context.Context, Squad) (Squad, error)
	GetSquad(context.Context, string, string) (Squad, error)
	ListSquads(context.Context, string) ([]Squad, error)
	UpdateSquad(context.Context, Squad) (SquadUpdateResult, error)
	DeleteSquad(context.Context, string, string) error
}
