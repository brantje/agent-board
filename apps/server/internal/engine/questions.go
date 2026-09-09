package engine

import "errors"

// ErrWaitingForInput is returned after a blocking Question has been durably
// persisted. Callers must treat it as a pause outcome rather than an execution
// failure.
var ErrWaitingForInput = errors.New("engine: waiting for input")

// ErrNotAttachable means the launcher supports Attach but this request has no
// live Execution Session. Adapters must Start a new process instead of treating
// that as an attach failure.
var ErrNotAttachable = errors.New("engine: execution session is not attachable")
