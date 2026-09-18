package store

import "context"

// InactiveRunCancellationStore terminalizes a Run that is durably inactive.
// Active execution remains owned by the scheduler worker and must use its
// cancellation boundary instead.
type InactiveRunCancellationStore interface {
	CancelInactiveRun(context.Context, string, string) (RunCancellationResult, error)
}

type RunCancellationResult struct {
	Run    Run
	Event  Event
	Events []Event
}
