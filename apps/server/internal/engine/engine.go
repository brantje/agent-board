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

// ProcessAttacher is an optional launcher capability for reconciling a live
// Execution Session after control-plane restart. Adapters must not Start a
// second process when Attach succeeds. Attach must return ErrNotAttachable
// when this request has no live session so adapters can Start instead.
type ProcessAttacher interface {
	Attach(context.Context) (Process, error)
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
	Custom         bool
}

type CorrelatedQuestionRequest struct {
	CorrelationKey string
	Question       QuestionRequest
}

type Question struct {
	ID       string
	Blocking bool
	Custom   bool
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

// IssueStatusUpdater is the narrow server-owned capability for an executing
// Agent to explicitly update only the Board status of its current Issue.
type IssueStatusUpdater interface {
	SetStatus(context.Context, string) error
}

type DelegationRequest struct {
	TargetAgentID string
	Task          string
	RequestKey    string
}

type Delegation struct {
	ID    string
	RunID string
}

type DelegationTargetContext struct {
	ID   string
	Name string
}

type SquadDelegationMemberContext struct {
	ID   string
	Name string
	Role *string
}

type SquadDelegationContext struct {
	ID              string
	Name            string
	LeaderAgentID   string
	LeaderAgentName string
	Members         []SquadDelegationMemberContext
}

type DelegationToolContext struct {
	Targets []DelegationTargetContext
	Squad   *SquadDelegationContext
}

// DelegationContinuation is bounded server-owned input for resuming a parent
// Run after one delegated child reaches a durable terminal outcome. It is
// execution-time state, not immutable Run provenance.
type DelegationContinuation struct {
	DelegationID             string
	TargetAgentID            string
	Task                     string
	Outcome                  string
	ResultSummary            string
	DelegatedRunID           string
	ResultEventID            string
	WorkspaceChangesAccepted bool
	WorkspaceRevision        string
}

// DelegationRequester is the narrow server-owned capability for a trusted
// Engine adapter to request bounded work from another Agent. Project, Issue,
// parent Run and parent Agent identities are always derived server-side.
type DelegationRequester interface {
	Delegate(context.Context, DelegationRequest) (Delegation, error)
}

// AcceptedDelegationResolver proves that a delegation request was already
// durably accepted without treating native Engine history as authority to
// create new work. Recovery callers must still supply the exact request
// identity observed from the native tool.
type AcceptedDelegationResolver interface {
	ResolveAcceptedDelegation(context.Context, DelegationRequest) (Delegation, bool, error)
}

type Request struct {
	Context                executioncontext.SafeContext
	Launcher               ProcessLauncher
	Questions              Questioner
	InteractiveQuestions   InteractiveQuestioner
	IssueStatus            IssueStatusUpdater
	Delegation             DelegationRequester
	DelegationContext      *DelegationToolContext
	Continuation           *Continuation
	DelegationContinuation *DelegationContinuation
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
