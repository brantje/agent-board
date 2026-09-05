package store

import (
	"encoding/json"
	"time"
)

var EmptyObject = json.RawMessage(`{}`)

type Project struct {
	ID               string
	Name             string
	RepositoryPath   string
	DefaultBranch    string
	WorkflowSettings json.RawMessage
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Issue struct {
	ID              string
	ProjectID       string
	Title           string
	Description     string
	Status          string
	AssignedAgentID *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Provider struct {
	ID            string
	Name          string
	Kind          string
	BaseURL       *string
	CredentialRef *string
	Enabled       bool
	HealthStatus  string
	SafeMetadata  json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
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

type ExecutorProfile struct {
	ID             string
	ProjectID      *string
	Name           string
	Engine         string
	ModelProfileID string
	RuntimeID      string
	EngineSettings json.RawMessage
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Agent struct {
	ID                string
	ProjectID         *string
	Name              string
	RoleInstructions  string
	ExecutorProfileID string
	ConcurrencyLimit  int
	State             string
	CreatedAt         time.Time
	UpdatedAt         time.Time
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

type Question struct {
	ID             string
	ProjectID      string
	IssueID        string
	RunID          string
	Prompt         string
	Kind           string
	Options        json.RawMessage
	Recommendation *string
	Blocking       bool
	Status         string
	CreatedAt      time.Time
	AnsweredAt     *time.Time
}

type Decision struct {
	ID          string
	ProjectID   string
	IssueID     *string
	RunID       *string
	QuestionID  *string
	Kind        string
	Outcome     string
	ActorType   string
	ActorID     *string
	SafeDetails json.RawMessage
	CreatedAt   time.Time
}

type Review struct {
	ID          string
	ProjectID   string
	IssueID     string
	RunID       string
	Status      string
	DecisionID  *string
	RequestedAt time.Time
	DecidedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Event struct {
	ID                string
	SchemaVersion     int
	Type              string
	OccurredAt        time.Time
	ProjectID         string
	IssueID           *string
	RunID             *string
	AgentID           *string
	WorkspaceID       *string
	RuntimeInstanceID *string
	CorrelationID     *string
	ParentEventID     *string
	Sequence          *int64
	Actor             json.RawMessage
	Payload           json.RawMessage
	CreatedAt         time.Time
}

type Artifact struct {
	ID           string
	ProjectID    string
	IssueID      string
	RunID        string
	Name         string
	Kind         string
	MediaType    *string
	SizeBytes    int64
	Digest       *string
	StorageRef   string
	SafeMetadata json.RawMessage
	CreatedAt    time.Time
	DeletedAt    *time.Time
}

type RawOutputChunk struct {
	ID         string
	ProjectID  string
	IssueID    string
	RunID      string
	Stream     string
	Sequence   int64
	StorageRef string
	SizeBytes  int64
	Digest     *string
	CreatedAt  time.Time
}
