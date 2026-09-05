package httpapi

import (
	"encoding/json"
	"time"
)

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ProjectDTO struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	RepositoryPath   string          `json:"repositoryPath"`
	DefaultBranch    string          `json:"defaultBranch"`
	WorkflowSettings json.RawMessage `json:"workflowSettings"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type IssueDTO struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"projectId"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Status          string    `json:"status"`
	AssignedAgentID *string   `json:"assignedAgentId"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type ProviderDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	BaseURL      *string         `json:"baseUrl"`
	Enabled      bool            `json:"enabled"`
	HealthStatus string          `json:"healthStatus"`
	SafeMetadata json.RawMessage `json:"safeMetadata"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

type ModelProfileDTO struct {
	ID                 string          `json:"id"`
	ProjectID          *string         `json:"projectId"`
	ProviderID         string          `json:"providerId"`
	Name               string          `json:"name"`
	Model              string          `json:"model"`
	Temperature        *float64        `json:"temperature"`
	MaxTokens          *int            `json:"maxTokens"`
	MaxConcurrent      *int            `json:"maxConcurrent"`
	GenerationSettings json.RawMessage `json:"generationSettings"`
	Enabled            bool            `json:"enabled"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
}

type RuntimeDTO struct {
	ID                string          `json:"id"`
	ProjectID         *string         `json:"projectId"`
	Name              string          `json:"name"`
	Kind              string          `json:"kind"`
	Image             string          `json:"image"`
	CPULimitMillis    *int            `json:"cpuLimitMillis"`
	MemoryLimitBytes  *int64          `json:"memoryLimitBytes"`
	PIDLimit          *int            `json:"pidLimit"`
	TimeoutSeconds    *int            `json:"timeoutSeconds"`
	NetworkPolicy     string          `json:"networkPolicy"`
	WorkspacePolicy   string          `json:"workspacePolicy"`
	AllowedSecretRefs []string        `json:"allowedSecretRefs"`
	Capabilities      json.RawMessage `json:"capabilities"`
	Enabled           bool            `json:"enabled"`
	HealthStatus      string          `json:"healthStatus"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type ExecutorProfileDTO struct {
	ID             string          `json:"id"`
	ProjectID      *string         `json:"projectId"`
	Name           string          `json:"name"`
	Engine         string          `json:"engine"`
	ModelProfileID string          `json:"modelProfileId"`
	RuntimeID      string          `json:"runtimeId"`
	EngineSettings json.RawMessage `json:"engineSettings"`
	Enabled        bool            `json:"enabled"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type AgentDTO struct {
	ID                string    `json:"id"`
	ProjectID         *string   `json:"projectId"`
	Name              string    `json:"name"`
	RoleInstructions  string    `json:"roleInstructions"`
	ExecutorProfileID string    `json:"executorProfileId"`
	ConcurrencyLimit  int       `json:"concurrencyLimit"`
	State             string    `json:"state"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type RunDTO struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"projectId"`
	IssueID       string     `json:"issueId"`
	WorkspaceID   string     `json:"workspaceId"`
	AgentID       *string    `json:"agentId"`
	Attempt       int        `json:"attempt"`
	Status        string     `json:"status"`
	QueueReason   *string    `json:"queueReason"`
	FailureReason *string    `json:"failureReason"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt"`
	CompletedAt   *time.Time `json:"completedAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type CreateProjectRequest struct {
	Name             string          `json:"name"`
	RepositoryPath   string          `json:"repositoryPath"`
	DefaultBranch    string          `json:"defaultBranch"`
	WorkflowSettings json.RawMessage `json:"workflowSettings"`
}

type UpdateProjectRequest struct {
	Name             *string          `json:"name"`
	RepositoryPath   *string          `json:"repositoryPath"`
	DefaultBranch    *string          `json:"defaultBranch"`
	WorkflowSettings *json.RawMessage `json:"workflowSettings"`
}

type CreateIssueRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type UpdateIssueRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}

type CreateProviderRequest struct {
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	BaseURL       *string         `json:"baseUrl"`
	CredentialRef *string         `json:"credentialRef"`
	Enabled       *bool           `json:"enabled"`
	SafeMetadata  json.RawMessage `json:"safeMetadata"`
}

type UpdateProviderRequest struct {
	Name          *string          `json:"name"`
	Kind          *string          `json:"kind"`
	BaseURL       *string          `json:"baseUrl"`
	CredentialRef *string          `json:"credentialRef"`
	Enabled       *bool            `json:"enabled"`
	SafeMetadata  *json.RawMessage `json:"safeMetadata"`
}

type CreateModelProfileRequest struct {
	ProviderID         string          `json:"providerId"`
	Name               string          `json:"name"`
	Model              string          `json:"model"`
	Temperature        *float64        `json:"temperature"`
	MaxTokens          *int            `json:"maxTokens"`
	MaxConcurrent      *int            `json:"maxConcurrent"`
	GenerationSettings json.RawMessage `json:"generationSettings"`
	Enabled            *bool           `json:"enabled"`
}

type CreateRuntimeRequest struct {
	Name              string          `json:"name"`
	Kind              string          `json:"kind"`
	Image             string          `json:"image"`
	CPULimitMillis    *int            `json:"cpuLimitMillis"`
	MemoryLimitBytes  *int64          `json:"memoryLimitBytes"`
	PIDLimit          *int            `json:"pidLimit"`
	TimeoutSeconds    *int            `json:"timeoutSeconds"`
	NetworkPolicy     string          `json:"networkPolicy"`
	AllowedSecretRefs []string        `json:"allowedSecretRefs"`
	Capabilities      json.RawMessage `json:"capabilities"`
	Enabled           *bool           `json:"enabled"`
}

type CreateExecutorProfileRequest struct {
	Name           string          `json:"name"`
	Engine         string          `json:"engine"`
	ModelProfileID string          `json:"modelProfileId"`
	RuntimeID      string          `json:"runtimeId"`
	EngineSettings json.RawMessage `json:"engineSettings"`
	Enabled        *bool           `json:"enabled"`
}

type CreateAgentRequest struct {
	Name              string `json:"name"`
	RoleInstructions  string `json:"roleInstructions"`
	ExecutorProfileID string `json:"executorProfileId"`
	ConcurrencyLimit  int    `json:"concurrencyLimit"`
	State             string `json:"state"`
}

type AssignmentRequest struct {
	AgentID string `json:"agentId"`
}

type AssignmentResponse struct {
	Issue IssueDTO `json:"issue"`
	Run   RunDTO   `json:"run"`
}
