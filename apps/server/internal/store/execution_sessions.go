package store

import "encoding/json"

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
