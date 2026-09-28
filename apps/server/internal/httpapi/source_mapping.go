package httpapi

import "github.com/brantje/agent-board/apps/server/internal/store"

func sourceConnectionDTO(v store.SourceConnection) SourceConnectionDTO {
	return SourceConnectionDTO{
		ID: v.ID,
		ProjectID: v.ProjectID,
		Kind: v.Kind,
		Name: v.Name,
		BaseURL: v.BaseURL,
		ExternalAccountID: v.ExternalAccountID,
		CredentialConfigured: v.CredentialRef != nil,
		Enabled: v.Enabled,
		HealthStatus: v.HealthStatus,
		LastValidatedAt: v.LastValidatedAt,
		CreatedAt: v.CreatedAt,
		UpdatedAt: v.UpdatedAt,
	}
}

func sourceRepositoryDTO(v store.SourceRepository) SourceRepositoryDTO {
	return SourceRepositoryDTO{
		ID: v.ID,
		SourceConnectionID: v.SourceConnectionID,
		ExternalID: v.ExternalID,
		Namespace: v.Namespace,
		Name: v.Name,
		Path: v.Path,
		WebURL: v.WebURL,
		CloneURL: v.CloneURL,
		SSHCloneURL: v.SSHCloneURL,
		DefaultBranch: v.DefaultBranch,
		Archived: v.Archived,
		Disabled: v.Disabled,
		LastSyncedAt: v.LastSyncedAt,
		CreatedAt: v.CreatedAt,
		UpdatedAt: v.UpdatedAt,
	}
}
