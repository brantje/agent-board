package store

import (
	"context"
	"encoding/json"
	"time"
)

// Runner is deployment-global execution capacity. Connectivity is never stored.
type Runner struct {
	ID                    string
	Name                  string
	TokenHash             []byte `json:"-"`
	RegistrationTokenHash []byte `json:"-"`
	Internal              bool
	RegisteredAt          *time.Time
	RevokedAt             *time.Time
	DeletedAt             *time.Time
	LastSeenAt            *time.Time
	Capabilities          json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type RunnerStore interface {
	CreateRunner(context.Context, Runner) (Runner, error)
	RegisterRunner(context.Context, []byte, Runner) (Runner, error)
	GetRunner(context.Context, string) (Runner, error)
	ListRunners(context.Context) ([]Runner, error)
	RenameRunner(context.Context, string, string) (Runner, error)
	UpdateRunner(context.Context, string, string, *int) (Runner, error)
	RotateRunnerCredential(context.Context, string, []byte) (Runner, error)
	RevokeRunner(context.Context, string, bool) (Runner, error)
	ObserveRunner(context.Context, string, json.RawMessage) error
	CountRunnerReservations(context.Context, []string) (map[string]int, error)
	ListProjectRunnerIDs(context.Context, string) ([]string, error)
	SetProjectRunnerIDs(context.Context, string, []string) error
}
