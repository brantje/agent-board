package httpapi

import "github.com/brantje/agent-board/apps/server/internal/store"

func projectDTO(v store.Project) ProjectDTO {
	return ProjectDTO{
		AllowInternalRunner: boolDefault(v.AllowInternalRunner, true),
		ID:                  v.ID,
		Name:                v.Name,
		IssuePrefix:         v.IssuePrefix,
		SourceType:          v.SourceType,
		CloneURL:            v.CloneURL,
		SourceRef:           v.SourceRef,
		RepositoryPath:      v.RepositoryPath,
		DefaultBranch:       v.DefaultBranch,
		WorkflowSettings:    v.WorkflowSettings,
		CreatedAt:           v.CreatedAt,
		UpdatedAt:           v.UpdatedAt,
	}
}

func issueDTO(v store.Issue) IssueDTO {
	dto := IssueDTO{v.Key, v.ProjectID, v.Number, v.Title, v.Description, v.Status, v.Priority, v.AssignedAgentID, v.CreatedAt, v.UpdatedAt, v.CurrentBranch, nil}
	if v.LastEvent != nil {
		event := eventEvidenceDTO(*v.LastEvent)
		dto.LastEvent = &event
	}
	return dto
}

func issueRelationshipDTO(v store.IssueRelationship, issueKeys map[string]string) IssueRelationshipDTO {
	return IssueRelationshipDTO{
		v.ID,
		v.ProjectID,
		issueKeyForUUID(issueKeys, v.SourceIssueID),
		issueKeyForUUID(issueKeys, v.TargetIssueID),
		v.Type,
		v.CreatedAt,
	}
}

func providerDTO(v store.Provider) ProviderDTO {
	return ProviderDTO{v.ID, v.ProjectID, v.Name, v.Kind, v.BaseURL, v.Enabled, v.HealthStatus, v.SafeMetadata, v.CreatedAt, v.UpdatedAt}
}

func modelProfileDTO(v store.ModelProfile) ModelProfileDTO {
	return ModelProfileDTO{v.ID, v.ProjectID, v.ProviderID, v.Name, v.Model, v.Temperature, v.MaxTokens, v.MaxConcurrent, v.GenerationSettings, v.Enabled, v.CreatedAt, v.UpdatedAt}
}

func runtimeDTO(v store.Runtime) RuntimeDTO {
	return RuntimeDTO{v.ID, v.ProjectID, v.Name, v.Kind, v.Image, v.CPULimitMillis, v.MemoryLimitBytes, v.PIDLimit, v.TimeoutSeconds, v.NetworkPolicy, v.WorkspacePolicy, v.AllowedSecretRefs, v.Capabilities, v.Enabled, v.HealthStatus, v.CreatedAt, v.UpdatedAt}
}

func agentDTO(v store.Agent) AgentDTO {
	return AgentDTO{v.ID, v.ProjectID, v.Name, v.RoleInstructions, v.Engine, v.ModelProfileID, v.EngineSettings, v.ConcurrencyLimit, v.State, v.CreatedAt, v.UpdatedAt}
}

func runDTO(v store.Run, issueKeys map[string]string) RunDTO {
	return RunDTO{v.ID, v.ProjectID, issueKeyForUUID(issueKeys, v.IssueID), v.WorkspaceID, v.AgentID, v.Attempt, v.Status, v.QueueReason, v.FailureReason, v.CreatedAt, v.StartedAt, v.CompletedAt, v.UpdatedAt, v.CurrentBranch}
}
