package httpapi

import "github.com/brantje/agent-board/apps/server/internal/store"

func projectDTO(v store.Project) ProjectDTO {
	return ProjectDTO{v.ID, v.Name, v.RepositoryPath, v.DefaultBranch, v.WorkflowSettings, v.CreatedAt, v.UpdatedAt}
}
func issueDTO(v store.Issue) IssueDTO {
	return IssueDTO{v.ID, v.ProjectID, v.Title, v.Description, v.Status, v.AssignedAgentID, v.CreatedAt, v.UpdatedAt}
}
func providerDTO(v store.Provider) ProviderDTO {
	return ProviderDTO{v.ID, v.Name, v.Kind, v.BaseURL, v.Enabled, v.HealthStatus, v.SafeMetadata, v.CreatedAt, v.UpdatedAt}
}
func modelProfileDTO(v store.ModelProfile) ModelProfileDTO {
	return ModelProfileDTO{v.ID, v.ProjectID, v.ProviderID, v.Name, v.Model, v.Temperature, v.MaxTokens, v.MaxConcurrent, v.GenerationSettings, v.Enabled, v.CreatedAt, v.UpdatedAt}
}
func runtimeDTO(v store.Runtime) RuntimeDTO {
	return RuntimeDTO{v.ID, v.ProjectID, v.Name, v.Kind, v.Image, v.CPULimitMillis, v.MemoryLimitBytes, v.PIDLimit, v.TimeoutSeconds, v.NetworkPolicy, v.WorkspacePolicy, v.AllowedSecretRefs, v.Capabilities, v.Enabled, v.HealthStatus, v.CreatedAt, v.UpdatedAt}
}
func executorProfileDTO(v store.ExecutorProfile) ExecutorProfileDTO {
	return ExecutorProfileDTO{v.ID, v.ProjectID, v.Name, v.Engine, v.ModelProfileID, v.RuntimeID, v.EngineSettings, v.Enabled, v.CreatedAt, v.UpdatedAt}
}
func agentDTO(v store.Agent) AgentDTO {
	return AgentDTO{v.ID, v.ProjectID, v.Name, v.RoleInstructions, v.ExecutorProfileID, v.ConcurrencyLimit, v.State, v.CreatedAt, v.UpdatedAt}
}
func runDTO(v store.Run) RunDTO {
	return RunDTO{v.ID, v.ProjectID, v.IssueID, v.WorkspaceID, v.AgentID, v.Attempt, v.Status, v.QueueReason, v.FailureReason, v.CreatedAt, v.StartedAt, v.CompletedAt, v.UpdatedAt}
}
