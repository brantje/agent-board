package engine

import (
	"context"
	"io"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

// ProcessLauncher is the only process capability exposed to Engine adapters.
// Implementations remain responsible for Runtime/runner transport details and
// the trusted execution-context/redaction boundary.
type ProcessLauncher interface {
	Start(context.Context, ProcessRequest) (Process, error)
}

type ProcessRequest struct {
	Command []string
	CWD     string
	Env     map[string]string
	Kind    string
	Name    string
}

type Process interface {
	ID() string
	Stdout() io.Reader
	Stderr() io.Reader
	Stdin() io.WriteCloser
	Wait(context.Context) (ProcessResult, error)
	Terminate(context.Context) error
	Kill(context.Context) error
}

type ProcessResult struct {
	ExitCode int
}

type Request struct {
	Context  executioncontext.SafeContext
	Launcher ProcessLauncher
}

type Result struct {
	Summary string
}

// Engine executes one Run attempt using only the resolved safe context and a
// narrow process launcher. Adapters never receive database, Docker, or raw
// runner/WebSocket access.
type Engine interface {
	Name() string
	Execute(context.Context, Request) (Result, error)
}
