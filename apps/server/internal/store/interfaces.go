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
}

type ConfigurationStore interface {
	CreateProvider(context.Context, Provider) (Provider, error)
	CreateModelProfile(context.Context, ModelProfile) (ModelProfile, error)
	CreateRuntime(context.Context, Runtime) (Runtime, error)
	CreateExecutorProfile(context.Context, ExecutorProfile) (ExecutorProfile, error)
	CreateAgent(context.Context, Agent) (Agent, error)
	GetAgent(context.Context, string, string) (Agent, error)
}

type ExecutionStore interface {
	CreateWorkspace(context.Context, Workspace) (Workspace, error)
	GetWorkspaceByIssue(context.Context, string, string) (Workspace, error)
	CreateRun(context.Context, Run) (Run, error)
	GetRun(context.Context, string, string) (Run, error)
	CreateRuntimeInstance(context.Context, RuntimeInstance) (RuntimeInstance, error)
	UpdateRuntimeInstanceState(context.Context, string, string, string, *string, string, json.RawMessage) (RuntimeInstance, error)
	CreateExecutionSession(context.Context, ExecutionSession) (ExecutionSession, error)
	CreateQuestion(context.Context, Question) (Question, error)
	CreateDecision(context.Context, Decision) (Decision, error)
	CreateReview(context.Context, Review) (Review, error)
}

type SchedulerStore interface {
	EnqueueJob(context.Context, SchedulerJob) (SchedulerJob, error)
	ClaimNextJob(context.Context, string, time.Duration) (*SchedulerJob, *SchedulerLease, error)
	RenewLease(context.Context, string, string, string, time.Duration) (SchedulerLease, error)
	ReleaseLease(context.Context, string, string, string) error
	ReserveCapacity(context.Context, string, string, string, string, string) error
	ReleaseCapacity(context.Context, string, string) error
}

type EvidenceStore interface {
	PutRunProvenance(context.Context, string, string, json.RawMessage) error
	GetRunProvenance(context.Context, string, string) (json.RawMessage, error)
	AppendEvent(context.Context, Event) (Event, error)
	ListRunEvents(context.Context, string, string, int64, int) ([]Event, error)
	CreateRawOutputChunk(context.Context, RawOutputChunk) (RawOutputChunk, error)
	CreateArtifact(context.Context, Artifact) (Artifact, error)
	ListArtifacts(context.Context, string, string) ([]Artifact, error)
}
