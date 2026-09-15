package store

import (
	"encoding/json"
	"time"
)

var EmptyObject = json.RawMessage(`{}`)

const (
	ProjectSourceLocal = "local"
	ProjectSourceGit   = "git"
	ActorTypeHuman     = "HUMAN"
	ActorTypeAgent     = "AGENT"
)

func ValidActorType(value string) bool {
	return value == ActorTypeHuman || value == ActorTypeAgent
}

type Project struct {
	AllowInternalRunner *bool
	ID                  string
	Name                string
	IssuePrefix         string
	SourceType          string
	CloneURL            *string
	SourceRef           *string
	RepositoryPath      string
	DefaultBranch       string
	WorkflowSettings    json.RawMessage
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Issue struct {
	ID            string
	ProjectID     string
	Number        int
	Key           string
	Title         string
	Description   string
	Status        string
	Priority      int
	BoardPosition int64
	AssigneeType  *string
	AssigneeID    *string
	AssigneeName  *string
	CreatedByType *string
	CreatedByID   *string
	CreatedByName *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastEvent     *Event
	CurrentBranch *string
}

type IssueRelationship struct {
	ID            string
	ProjectID     string
	SourceIssueID string
	TargetIssueID string
	Type          string
	CreatedAt     time.Time
}

type Provider struct {
	ID                 string
	ProjectID          *string
	Name               string
	Kind               string
	BaseURL            *string
	CredentialRef      *string
	Enabled            bool
	HealthStatus       string
	FilteredModelCount *int
	TotalModelCount    *int
	SafeMetadata       json.RawMessage
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ModelProfile struct {
	ID                 string
	ProjectID          *string
	ProviderID         string
	Name               string
	Model              string
	Temperature        *float64
	MaxTokens          *int
	MaxConcurrent      *int
	GenerationSettings json.RawMessage
	Enabled            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Runtime struct {
	ID                string
	ProjectID         *string
	Name              string
	Kind              string
	Image             string
	CPULimitMillis    *int
	MemoryLimitBytes  *int64
	PIDLimit          *int
	TimeoutSeconds    *int
	NetworkPolicy     string
	WorkspacePolicy   string
	AllowedSecretRefs []string
	Capabilities      json.RawMessage
	Enabled           bool
	HealthStatus      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Agent struct {
	ID               string
	ProjectID        *string
	Name             string
	RoleInstructions string
	Engine           string
	ModelProfileID   string
	EngineSettings   json.RawMessage
	ConcurrencyLimit int
	State            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Workspace struct {
	ID              string
	ProjectID       string
	IssueID         string
	Path            string
	RepositoryPath  *string
	BaseBranch      *string
	BaseRevision    *string
	WorkingBranch   string
	CurrentBranch   *string
	BootstrapStatus string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Run struct {
	ID            string
	ProjectID     string
	IssueID       string
	WorkspaceID   string
	AgentID       *string
	Attempt       int
	Status        string
	QueueReason   *string
	FailureReason *string
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	UpdatedAt     time.Time
	CurrentBranch *string
}

type RuntimeInstance struct {
	ID                 string
	ProjectID          string
	WorkspaceID        string
	RuntimeID          string
	Status             string
	ExternalID         *string
	RunnerStatus       string
	SafeHandleMetadata json.RawMessage
	CreatedAt          time.Time
	StartedAt          *time.Time
	StoppedAt          *time.Time
	UpdatedAt          time.Time
}

type ExecutionSession struct {
	ID                string
	ProjectID         string
	RunID             string
	RuntimeInstanceID string
	RunnerID          string
	Status            string
	CWD               string
	CommandArgv       json.RawMessage
	ExitCode          *int
	CreatedAt         time.Time
	StartedAt         *time.Time
	CompletedAt       *time.Time
	UpdatedAt         time.Time
}

type SchedulerJob struct {
	ID             string
	ProjectID      string
	RunID          string
	Kind           string
	State          string
	WaitReason     *string
	IdempotencyKey string
	AvailableAt    time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SchedulerLease struct {
	JobID      string
	OwnerID    string
	LeaseToken string
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

const (
	SchedulerWaitAgentCapacity = "agent_capacity"
	SchedulerWaitModelCapacity = "model_capacity"
	SchedulerWaitWorkspace     = "workspace_occupied"
)
