package engine

import "errors"

// ErrWaitingForInput is returned after a blocking Question has been durably
// persisted. Callers must treat it as a pause outcome rather than an execution
// failure.
var ErrWaitingForInput = errors.New("engine: waiting for input")
