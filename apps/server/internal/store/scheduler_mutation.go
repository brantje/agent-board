package store

import "context"

// SchedulerMutationResult exposes Events persisted atomically with a scheduler
// transition so callers can publish them only after the store has committed.
type SchedulerMutationResult struct {
	Run    Run
	Events []Event
}

// SchedulerMutationStore is the transaction-aware scheduler mutation boundary
// used when a transition can persist externally visible durable Events.
type SchedulerMutationStore interface {
	TransitionAdmittedJobMutation(context.Context, SchedulerTransition) (SchedulerMutationResult, error)
	ResolveReconciliationMutation(context.Context, SchedulerReconciliation) (SchedulerMutationResult, error)
}
