package mcpapi

import "time"

type EmptyInput struct{}

type ProjectInput struct {
	ProjectID string `json:"projectId" jsonschema:"Agent Board Project UUID"`
}

type AgentInput struct {
	ProjectID string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	AgentID   string `json:"agentId" jsonschema:"Agent UUID"`
}

type IssueInput struct {
	ProjectID string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	IssueID   string `json:"issueId" jsonschema:"public Issue key, for example AB-123"`
}

type RunInput struct {
	ProjectID string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	RunID     string `json:"runId" jsonschema:"Run UUID"`
}

type ProjectDTO struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	IssuePrefix         string    `json:"issuePrefix"`
	SourceType          string    `json:"sourceType"`
	DefaultBranch       string    `json:"defaultBranch"`
	AllowInternalRunner *bool     `json:"allowInternalRunner,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type ProjectRoleDTO struct {
	Role string `json:"role"`
}

type ProjectMemberDTO struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

type AgentDTO struct {
	ID               string    `json:"id"`
	ProjectID        *string   `json:"projectId,omitempty"`
	Name             string    `json:"name"`
	RoleInstructions string    `json:"roleInstructions"`
	Engine           string    `json:"engine"`
	ModelProfileID   string    `json:"modelProfileId"`
	ConcurrencyLimit int       `json:"concurrencyLimit"`
	State            string    `json:"state"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type AssigneeDTO struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ActorDTO struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EventSummaryDTO struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurredAt"`
}

type IssueDTO struct {
	ID            string           `json:"id"`
	ProjectID     string           `json:"projectId"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Status        string           `json:"status"`
	Priority      int              `json:"priority"`
	AssignedTo    *AssigneeDTO     `json:"assignedTo"`
	CreatedBy     *ActorDTO        `json:"createdBy,omitempty"`
	CurrentBranch *string          `json:"currentBranch,omitempty"`
	LastEvent     *EventSummaryDTO `json:"lastEvent,omitempty"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

type CreateIssueInput struct {
	ProjectID  string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	Title      string `json:"title" jsonschema:"Issue title"`
	Description string `json:"description,omitempty" jsonschema:"Issue description"`
	Priority   *int   `json:"priority,omitempty" jsonschema:"priority from 0 through 4"`
}

type UpdateIssueInput struct {
	ProjectID   string  `json:"projectId" jsonschema:"Agent Board Project UUID"`
	IssueID     string  `json:"issueId" jsonschema:"public Issue key, for example AB-123"`
	Title       *string `json:"title,omitempty" jsonschema:"replacement Issue title"`
	Description *string `json:"description,omitempty" jsonschema:"replacement Issue description"`
	Priority    *int    `json:"priority,omitempty" jsonschema:"priority from 0 through 4"`
}

type IssueStatus string

type SetIssueStatusInput struct {
	ProjectID string      `json:"projectId" jsonschema:"Agent Board Project UUID"`
	IssueID   string      `json:"issueId" jsonschema:"public Issue key, for example AB-123"`
	Status    IssueStatus `json:"status" jsonschema:"explicit Board status"`
}

type AssigneeType string

type AssigneeTarget struct {
	Type AssigneeType `json:"type" jsonschema:"ownership target type"`
	ID   string       `json:"id" jsonschema:"User or Agent UUID"`
}

type SetIssueAssigneeInput struct {
	ProjectID  string          `json:"projectId" jsonschema:"Agent Board Project UUID"`
	IssueID    string          `json:"issueId" jsonschema:"public Issue key, for example AB-123"`
	AssignedTo *AssigneeTarget `json:"assignedTo" jsonschema:"USER or AGENT target; null clears ownership"`
}

type RelationshipType string

type RelationshipDTO struct {
	ID            string           `json:"id"`
	ProjectID     string           `json:"projectId"`
	SourceIssueID string           `json:"sourceIssueId"`
	TargetIssueID string           `json:"targetIssueId"`
	Type          RelationshipType `json:"type"`
	CreatedAt     time.Time        `json:"createdAt"`
}

type CreateRelationshipInput struct {
	ProjectID     string           `json:"projectId" jsonschema:"Agent Board Project UUID"`
	IssueID       string           `json:"issueId" jsonschema:"source public Issue key"`
	TargetIssueID string           `json:"targetIssueId" jsonschema:"target public Issue key"`
	Type          RelationshipType `json:"type" jsonschema:"relationship type"`
}

type DeleteRelationshipInput struct {
	ProjectID      string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	IssueID        string `json:"issueId" jsonschema:"source public Issue key"`
	RelationshipID string `json:"relationshipId" jsonschema:"relationship UUID"`
}

type MutationAck struct {
	Success bool `json:"success"`
}

type IssueExecutionStateDTO struct {
	State     string  `json:"state"`
	CanStart  bool    `json:"canStart"`
	ActiveRun *RunDTO `json:"activeRun"`
}

type RunDTO struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"projectId"`
	IssueID       string     `json:"issueId"`
	AgentID       *string    `json:"agentId,omitempty"`
	Attempt       int        `json:"attempt"`
	Status        string     `json:"status"`
	QueueReason   *string    `json:"queueReason,omitempty"`
	FailureReason *string    `json:"failureReason,omitempty"`
	CurrentBranch *string    `json:"currentBranch,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type RuntimeInstanceDTO struct {
	ID           string     `json:"id"`
	RuntimeID    string     `json:"runtimeId"`
	Status       string     `json:"status"`
	RunnerStatus string     `json:"runnerStatus"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	StoppedAt    *time.Time `json:"stoppedAt,omitempty"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type ExecutionSessionDTO struct {
	ID                string     `json:"id"`
	RuntimeInstanceID *string    `json:"runtimeInstanceId,omitempty"`
	RunnerID          *string    `json:"runnerId,omitempty"`
	Status            string     `json:"status"`
	ExitCode          *int       `json:"exitCode,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type EventDTO struct {
	ID                string    `json:"id"`
	SchemaVersion     int       `json:"schemaVersion"`
	Type              string    `json:"type"`
	OccurredAt        time.Time `json:"occurredAt"`
	ProjectID         string    `json:"projectId"`
	IssueID           *string   `json:"issueId,omitempty"`
	RunID             *string   `json:"runId,omitempty"`
	AgentID           *string   `json:"agentId,omitempty"`
	WorkspaceID       *string   `json:"workspaceId,omitempty"`
	RuntimeInstanceID *string   `json:"runtimeInstanceId,omitempty"`
	CorrelationID     *string   `json:"correlationId,omitempty"`
	ParentEventID     *string   `json:"parentEventId,omitempty"`
	Sequence          *int64    `json:"sequence,omitempty"`
	Actor             any       `json:"actor,omitempty"`
	Payload           any       `json:"payload,omitempty"`
}

type RawOutputChunkDTO struct {
	ID        string    `json:"id"`
	Stream    string    `json:"stream"`
	Sequence  int64     `json:"sequence"`
	SizeBytes int64     `json:"sizeBytes"`
	Digest    *string   `json:"digest,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type ArtifactDTO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	MediaType    *string   `json:"mediaType,omitempty"`
	SizeBytes    int64     `json:"sizeBytes"`
	Digest       *string   `json:"digest,omitempty"`
	SafeMetadata any       `json:"safeMetadata,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type RunEvidenceDTO struct {
	Run              RunDTO                `json:"run"`
	Provenance       any                   `json:"provenance,omitempty"`
	RuntimeInstances []RuntimeInstanceDTO  `json:"runtimeInstances"`
	Sessions         []ExecutionSessionDTO `json:"executionSessions"`
	Events           []EventDTO            `json:"events"`
	Tests            []EventDTO            `json:"tests"`
	FileChanges      []EventDTO            `json:"fileChanges"`
	Usage            any                   `json:"usage,omitempty"`
	RawOutput        []RawOutputChunkDTO    `json:"rawOutput"`
	Artifacts        []ArtifactDTO          `json:"artifacts"`
}

type ReadRunOutputInput struct {
	ProjectID string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	RunID     string `json:"runId" jsonschema:"Run UUID"`
	ChunkID   string `json:"chunkId" jsonschema:"raw-output chunk UUID"`
}

type ReadRunOutputDTO struct {
	Chunk   RawOutputChunkDTO `json:"chunk"`
	Content string            `json:"content"`
}

type ListQuestionsInput struct {
	ProjectID string   `json:"projectId" jsonschema:"Agent Board Project UUID"`
	IssueID   *string  `json:"issueId,omitempty" jsonschema:"optional public Issue key"`
	RunID     *string  `json:"runId,omitempty" jsonschema:"optional Run UUID"`
	Statuses  []string `json:"statuses,omitempty" jsonschema:"optional Question status filters"`
}

type QuestionInput struct {
	ProjectID  string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	QuestionID string `json:"questionId" jsonschema:"Question UUID"`
}

type QuestionDTO struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"projectId"`
	IssueID        string     `json:"issueId"`
	RunID          string     `json:"runId"`
	Prompt         string     `json:"prompt"`
	Kind           string     `json:"kind"`
	Options        any        `json:"options,omitempty"`
	Recommendation *string    `json:"recommendation,omitempty"`
	Custom         bool       `json:"custom"`
	Blocking       bool       `json:"blocking"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"createdAt"`
	AnsweredAt     *time.Time `json:"answeredAt,omitempty"`
}

type AnswerQuestionInput struct {
	ProjectID  string   `json:"projectId" jsonschema:"Agent Board Project UUID"`
	QuestionID string   `json:"questionId" jsonschema:"Question UUID"`
	Kind       string   `json:"kind" jsonschema:"answer kind expected by the Question"`
	Text       *string  `json:"text,omitempty"`
	OptionIDs  []string `json:"optionIds,omitempty"`
}

type DecisionDTO struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	IssueID     *string   `json:"issueId,omitempty"`
	RunID       *string   `json:"runId,omitempty"`
	QuestionID  *string   `json:"questionId,omitempty"`
	Kind        string    `json:"kind"`
	Outcome     string    `json:"outcome"`
	ActorType   string    `json:"actorType"`
	ActorID     *string   `json:"actorId,omitempty"`
	SafeDetails any       `json:"safeDetails,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AnswerQuestionDTO struct {
	Question           QuestionDTO `json:"question"`
	Decision           DecisionDTO `json:"decision"`
	Run                RunDTO      `json:"run"`
	ContinuationQueued bool        `json:"continuationQueued"`
}

type ListReviewsInput struct {
	ProjectID string   `json:"projectId" jsonschema:"Agent Board Project UUID"`
	IssueID   *string  `json:"issueId,omitempty" jsonschema:"optional public Issue key"`
	Statuses  []string `json:"statuses,omitempty" jsonschema:"optional Review status filters"`
}

type ReviewInput struct {
	ProjectID string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	ReviewID  string `json:"reviewId" jsonschema:"Review UUID"`
}

type ReviewDTO struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"projectId"`
	IssueID        string     `json:"issueId"`
	RunID          string     `json:"runId"`
	Status         string     `json:"status"`
	DecisionID     *string    `json:"decisionId,omitempty"`
	BaseRevision   string     `json:"baseRevision"`
	ReviewRevision string     `json:"reviewRevision"`
	RequestedAt    time.Time  `json:"requestedAt"`
	DecidedAt      *time.Time `json:"decidedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ReviewInspectionDTO struct {
	Review     ReviewDTO      `json:"review"`
	Decision   *DecisionDTO   `json:"decision,omitempty"`
	Evidence   RunEvidenceDTO `json:"evidence"`
	TestStatus string         `json:"testStatus"`
}

type ReviewDecisionDTO struct {
	Review   ReviewDTO   `json:"review"`
	Decision DecisionDTO `json:"decision"`
	Run      RunDTO      `json:"run"`
	Issue    IssueDTO    `json:"issue"`
}

type RequestReviewChangesInput struct {
	ProjectID string `json:"projectId" jsonschema:"Agent Board Project UUID"`
	ReviewID  string `json:"reviewId" jsonschema:"Review UUID"`
	Feedback  string `json:"feedback" jsonschema:"human Review feedback persisted with the Decision"`
}
