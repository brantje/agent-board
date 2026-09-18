package engine

import "context"

// ActivityEvent is an explicitly emitted, user-visible Engine activity mapped
// onto one of Agent Board's canonical event types. Adapters must not populate
// it from inferred/private reasoning.
type ActivityEvent struct {
	Type    string
	Payload map[string]any
}

// ActivitySink is an optional server-owned capability exposed by a
// ProcessLauncher. The run-execution layer validates allowed event types before
// persistence, so adapters never receive direct evidence-store access.
type ActivitySink interface {
	RecordActivity(context.Context, ActivityEvent) error
}

// ExecutionAdmissionPromptStore binds the first initial prompt to the durable
// execution session that owns a native Engine process. Recovered executions use
// the stored prompt instead of reconstructing identity from mutable context.
type ExecutionAdmissionPromptStore interface {
	GetOrCreateAdmissionPrompt(context.Context, string, string) (string, error)
}
