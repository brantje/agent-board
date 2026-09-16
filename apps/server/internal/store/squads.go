package store

import (
	"context"
	"time"
)

const (
	SquadMemberTypeAgent = "AGENT"
	SquadMemberTypeUser  = "USER"
)

// Squad is durable Project-scoped collaboration configuration. LeaderAgentID is
// the canonical leadership and execution reference; Members contains additional
// Agent or User identities with optional descriptive roles.
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
	Type string
	ID   string
	Role *string
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
