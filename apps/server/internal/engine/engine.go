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

type CorrelatedQuestionRequest struct {
	CorrelationKey string
	Question       QuestionRequest
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

type AcceptedInteractiveQuestionReply struct {
	QuestionID     string `json:"questionId"`
	CorrelationKey string `json:"correlationKey"`
}

type Continuation struct {
	QuestionID string
	DecisionID string
	Prompt     string
	Answer     QuestionAnswer
}

// Questioner is the recovery-oriented human-input capability. A blocking Ask
// unwinds the Engine so the durable scheduler can later execute a continuation.
type Questioner interface {
	Ask(context.Context, QuestionRequest) (Question, error)
}

// InteractiveQuestioner keeps a native Engine session alive while durable
// human input is collected. Correlation keys are opaque Engine-owned values;
// persistence, Run state and recovery remain server-owned.
type InteractiveQuestioner interface {
	Open(context.Context, string, QuestionRequest) (Question, error)
	WaitAnswer(context.Context, string) (QuestionAnswer, error)
	Resolve(context.Context, string) error
}

// InteractiveQuestionBatcher opens one native Question request atomically. It
// is a separate capability so existing single-Question adapters remain valid
// while engines with multi-Question native requests can require batch safety.
type InteractiveQuestionBatcher interface {
	OpenBatch(context.Context, []CorrelatedQuestionRequest) ([]Question, error)
}

// InteractiveQuestionReplyTracker durably journals that a native engine has
// accepted a reply before canonical bindings are resolved. ListReplyAccepted
// returns journaled bindings that still need a durable resolution marker.
type InteractiveQuestionReplyTracker interface {
	MarkReplyAccepted(context.Context, []AcceptedInteractiveQuestionReply) error
	ListReplyAccepted(context.Context) ([]AcceptedInteractiveQuestionReply, error)
}

type Request struct {
	Context              executioncontext.SafeContext
	Launcher             ProcessLauncher
	Questions            Questioner
	InteractiveQuestions InteractiveQuestioner
	Continuation         *Continuation
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
