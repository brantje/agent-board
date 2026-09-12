package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound        = errors.New("store: not found")
	ErrConflict        = errors.New("store: conflict")
	ErrInvalidArgument = errors.New("store: invalid argument")
)

type CoreStore interface {
	CreateProject(context.Context, Project) (Project, error)
	GetProject(context.Context, string) (Project, error)
	CreateIssue(context.Context, Issue) (Issue, error)
	GetIssue(context.Context, string, string) (Issue, error)
	GetIssueUUIDByKey(context.Context, string, string) (string, error)
}

type ConfigurationStore interface {
	CreateProvider(context.Context, Provider) (Provider, error)
	CreateModelProfile(context.Context, ModelProfile) (ModelProfile, error)
	CreateAgent(context.Context, Agent) (Agent, error)
	GetAgent(context.Context, string, string) (Agent, error)
}

type ExecutionStore interface {
	CreateWorkspace(context.Context, Workspace) (Workspace, error)
	GetWorkspaceByIssue(context.Context, string, string) (Workspace, error)
	GetWorkspace(context.Context, string, string) (Workspace, error)
	UpdateWorkspaceCurrentBranch(context.Context, string, string, string) (Workspace, error)
	CreateRun(context.Context, Run) (Run, error)
	GetRun(context.Context, string, string) (Run, error)
	CreateExecutionSession(context.Context, ExecutionSession) (ExecutionSession, error)
	GetExecutionSession(context.Context, string, string) (ExecutionSession, error)
	ListExecutionSessions(context.Context, string, []string) ([]ExecutionSession, error)
	ListExecutionSessionsByRun(context.Context, string, string, []string) ([]ExecutionSession, error)
	ListExecutionSessionsByRunner(context.Context, string, []string) ([]ExecutionSession, error)
	TransitionExecutionSession(context.Context, ExecutionSessionTransition) (ExecutionSession, error)
	CreateQuestion(context.Context, Question) (Question, error)
	CreateDecision(context.Context, Decision) (Decision, error)
	CreateReview(context.Context, Review) (Review, error)
}

type SchedulerStore interface {
	EnqueueJob(context.Context, SchedulerJob) (SchedulerJob, error)
	AdmitNextJob(context.Context, string, time.Duration, time.Duration) (*SchedulerAdmission, error)
	RenewLease(context.Context, string, string, string, time.Duration) (SchedulerLease, error)
	TransitionAdmittedJob(context.Context, SchedulerTransition) (Run, error)
	ClaimExpiredJobForReconciliation(context.Context, string, time.Duration) (*SchedulerAdmission, error)
	ResolveReconciliation(context.Context, SchedulerReconciliation) (Run, error)
}

type EvidenceStore interface {
	PutRunProvenance(context.Context, string, string, json.RawMessage) error
	GetRunProvenance(context.Context, string, string) (json.RawMessage, error)
	AppendEvent(context.Context, Event) (Event, error)
	ListRunEvents(context.Context, string, string, int64, int) ([]Event, error)
	ListProjectEventsAfter(context.Context, string, string, int) ([]Event, error)
	CreateRawOutputChunk(context.Context, RawOutputChunk) (RawOutputChunk, error)
	GetRawOutputChunk(context.Context, string, string, string) (RawOutputChunk, error)
	ListRawOutputChunks(context.Context, string, string) ([]RawOutputChunk, error)
	CreateArtifact(context.Context, Artifact) (Artifact, error)
	GetArtifact(context.Context, string, string, string) (Artifact, error)
	ListArtifacts(context.Context, string, string) ([]Artifact, error)
}
