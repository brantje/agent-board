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
	Command               []string
	CWD                   string
	Env                   map[string]string
	ProviderCredentialEnv string
	RuntimeSecretRefs     map[string]string
	Kind                  string
	Name                  string
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

type QuestionOption struct {
	ID    string
	Label string
}

type QuestionRequest struct {
	Prompt         string
	Kind           string
	Options        []QuestionOption
	Recommendation *string
	Blocking       bool
}

type Question struct {
	ID       string
	Blocking bool
}

type QuestionAnswer struct {
	Kind      string
	Text      *string
	OptionIDs []string
}

type Continuation struct {
	QuestionID string
	DecisionID string
	Prompt     string
	Answer     QuestionAnswer
}

// Questioner is the narrow human-input capability available to Engine adapters.
// Durable Question persistence and Run lifecycle changes remain server-owned.
type Questioner interface {
	Ask(context.Context, QuestionRequest) (Question, error)
}

type Request struct {
	Context      executioncontext.SafeContext
	Launcher     ProcessLauncher
	Questions    Questioner
	Continuation *Continuation
}

type Result struct {
	Summary string
}

// Engine executes one Run attempt using only the resolved safe context and
// narrow server-owned capabilities. Adapters never receive database, Docker,
// scheduler, or raw runner/WebSocket access.
type Engine interface {
	Name() string
	Execute(context.Context, Request) (Result, error)
}
