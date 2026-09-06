package executioncontext

import "encoding/json"

type SafeContext struct {
	Project   ProjectContext   `json:"project"`
	Issue     IssueContext     `json:"issue"`
	Run       RunContext       `json:"run"`
	Agent     AgentContext     `json:"agent"`
	Executor  ExecutorContext  `json:"executor"`
	Model     ModelContext     `json:"model"`
	Provider  ProviderContext  `json:"provider"`
	Runtime   RuntimeContext   `json:"runtime"`
	Workspace WorkspaceContext `json:"workspace"`
}

type ProjectContext struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	RepositoryPath   string          `json:"repositoryPath"`
	DefaultBranch    string          `json:"defaultBranch"`
	WorkflowSettings json.RawMessage `json:"workflowSettings,omitempty"`
}

type IssueContext struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type RunContext struct {
	ID      string `json:"id"`
	Attempt int    `json:"attempt"`
}

type AgentContext struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	RoleInstructions string `json:"roleInstructions"`
}

type ExecutorContext struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Engine         string          `json:"engine"`
	EngineSettings json.RawMessage `json:"engineSettings,omitempty"`
}

type ModelContext struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Model              string          `json:"model"`
	Temperature        *float64        `json:"temperature,omitempty"`
	MaxTokens          *int            `json:"maxTokens,omitempty"`
	GenerationSettings json.RawMessage `json:"generationSettings,omitempty"`
}

type ProviderContext struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	BaseURL      *string         `json:"baseUrl,omitempty"`
	SafeMetadata json.RawMessage `json:"safeMetadata,omitempty"`
}

type RuntimeContext struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Kind             string          `json:"kind"`
	Image            string          `json:"image"`
	CPULimitMillis   *int            `json:"cpuLimitMillis,omitempty"`
	MemoryLimitBytes *int64          `json:"memoryLimitBytes,omitempty"`
	PIDLimit         *int            `json:"pidLimit,omitempty"`
	TimeoutSeconds   *int            `json:"timeoutSeconds,omitempty"`
	NetworkPolicy    string          `json:"networkPolicy"`
	WorkspacePolicy  string          `json:"workspacePolicy"`
	Capabilities     json.RawMessage `json:"capabilities,omitempty"`
}

type WorkspaceContext struct {
	ID             string  `json:"id"`
	Path           string  `json:"path"`
	RepositoryPath *string `json:"repositoryPath,omitempty"`
	BaseBranch     *string `json:"baseBranch,omitempty"`
	BaseRevision   *string `json:"baseRevision,omitempty"`
	WorkingBranch  string  `json:"workingBranch"`
	BootstrapStatus string `json:"bootstrapStatus"`
}

type Resolved struct {
	Safe                  SafeContext
	ProviderCredentialRef *string
	AllowedSecretRefs     []string
}
