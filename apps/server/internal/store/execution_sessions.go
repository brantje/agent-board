package store

import (
	"context"
	"encoding/json"
)

// ExecutionSessionTransition is a compare-and-set durable state transition.
// FromStatuses protects terminal state from late transport events and races.
type ExecutionSessionTransition struct {
	ProjectID    string
	SessionID    string
	FromStatuses []string
	Status       string
	ExitCode     *int
	CommandArgv  json.RawMessage
}

// ExecutionSessionAdmissionStore persists the immutable initial prompt for one
// durable execution session. Get-or-create semantics make the first execution
// identity authoritative across restart and mutable Run context.
type ExecutionSessionAdmissionStore interface {
	GetOrCreateExecutionSessionAdmissionPrompt(context.Context, string, string, string) (string, error)
}
