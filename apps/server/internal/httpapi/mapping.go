package httpapi

import (
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func nullableIssueCommentAgentID(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

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
	dto := IssueDTO{
		ID:            v.Key,
		ProjectID:     v.ProjectID,
		Number:        v.Number,
		Title:         v.Title,
		Description:   v.Description,
		Status:        v.Status,
		Priority:      v.Priority,
		AssignedTo:    v.AssignedTo(),
		CreatedAt:     v.CreatedAt,
		UpdatedAt:     v.UpdatedAt,
		CurrentBranch: v.CurrentBranch,
	}
	if v.CreatedByType != nil && v.CreatedByID != nil {
		dto.CreatedBy = &IssueCreatorDTO{Type: *v.CreatedByType, ID: *v.CreatedByID, Name: v.CreatedByName}
	}
	if v.LastEvent != nil {
		event := eventEvidenceDTO(*v.LastEvent)
		dto.LastEvent = &event
	}
	return dto
}

func issueCommentDTO(v store.IssueComment, issueKey, viewerID string) IssueCommentDTO {
	var body *string
	if v.DeletedAt == nil {
		body = &v.Body
	}
	var resolvedBy *IssueCommentResolverDTO
	if v.ResolvedByUserID != nil {
		resolvedBy = &IssueCommentResolverDTO{ID: *v.ResolvedByUserID, Name: v.ResolvedByName}
	}
	reactions := make([]IssueCommentReactionSummaryDTO, 0, len(v.Reactions))
	for _, reaction := range v.Reactions {
		reacted := false
		for _, actorID := range reaction.ActorIDs {
			if actorID == viewerID {
				reacted = true
				break
			}
		}
		reactions = append(reactions, IssueCommentReactionSummaryDTO{
			Reaction: reaction.Reaction, Count: reaction.Count, ReactedByCurrentUser: reacted,
		})
	}
	mentions := make([]IssueCommentMentionDTO, 0, len(v.Mentions))
	for _, mention := range v.Mentions {
		mentions = append(mentions, IssueCommentMentionDTO{
			TargetType: mention.Target.Type, TargetID: mention.Target.ID, TargetName: mention.TargetName,
			ResolvedAgentID: nullableIssueCommentAgentID(mention.ResolvedAgentID), ResolvedAgentName: mention.ResolvedAgentName,
			ID: mention.ID, TargetAgentID: mention.TargetAgentID, TargetAgentName: mention.TargetAgentName,
			Outcome: mention.Outcome, ReasonCode: mention.ReasonCode, DelegationID: mention.DelegationID, DelegatedRunID: mention.DelegatedRunID,
		})
	}
	var implicit *IssueCommentImplicitTriggerDTO
	if v.ImplicitTrigger != nil {
		implicit = &IssueCommentImplicitTriggerDTO{
			TargetType: v.ImplicitTrigger.Target.Type, TargetID: v.ImplicitTrigger.Target.ID, TargetName: v.ImplicitTrigger.TargetName,
			ResolvedAgentID: nullableIssueCommentAgentID(v.ImplicitTrigger.ResolvedAgentID), ResolvedAgentName: v.ImplicitTrigger.ResolvedAgentName,
			TargetAgentID: v.ImplicitTrigger.TargetAgentID, TargetAgentName: v.ImplicitTrigger.TargetAgentName,
			RoutingReason: v.ImplicitTrigger.RoutingReason, Outcome: v.ImplicitTrigger.Outcome,
			ReasonCode: v.ImplicitTrigger.ReasonCode, DelegationID: v.ImplicitTrigger.DelegationID,
			DelegatedRunID: v.ImplicitTrigger.DelegatedRunID,
		}
	}
	return IssueCommentDTO{
		ID:              v.ID,
		IssueID:         issueKey,
		ParentCommentID: v.ParentCommentID,
		SourceRunID:     v.SourceRunID,
		Author:          IssueCommentAuthorDTO{Type: v.AuthorType, ID: v.AuthorID, Name: v.AuthorName},
		Body:            body,
		DeletedAt:       v.DeletedAt,
		ResolvedAt:      v.ResolvedAt,
		ResolvedBy:      resolvedBy,
		Reactions:       reactions,
		Mentions:        mentions,
		ImplicitTrigger: implicit,
		CreatedAt:       v.CreatedAt,
		UpdatedAt:       v.UpdatedAt,
	}
}

func issueTimelineEntryDTO(v app.IssueTimelineEntry, issueKey, viewerID string) IssueTimelineEntryDTO {
	out := IssueTimelineEntryDTO{Kind: v.Kind, ID: v.ID, OccurredAt: v.OccurredAt}
	if v.Comment != nil {
		comment := issueCommentDTO(*v.Comment, issueKey, viewerID)
		out.Comment = &comment
	}
	if v.Event != nil {
		activity := eventEvidenceDTO(*v.Event)
		out.Activity = &activity
	}
	return out
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
	return ProviderDTO{v.ID, v.ProjectID, v.Name, v.Kind, v.BaseURL, v.Enabled, v.HealthStatus, v.FilteredModelCount, v.TotalModelCount, v.SafeMetadata, v.CreatedAt, v.UpdatedAt}
}

func modelProfileDTO(v store.ModelProfile) ModelProfileDTO {
	return ModelProfileDTO{v.ID, v.ProjectID, v.ProviderID, v.Name, v.Model, v.Temperature, v.MaxTokens, v.MaxConcurrent, v.GenerationSettings, v.Enabled, v.CreatedAt, v.UpdatedAt}
}

func runtimeDTO(v store.Runtime) RuntimeDTO {
	return RuntimeDTO{v.ID, v.ProjectID, v.Name, v.Kind, v.Image, v.CPULimitMillis, v.MemoryLimitBytes, v.PIDLimit, v.TimeoutSeconds, v.NetworkPolicy, v.WorkspacePolicy, v.AllowedSecretRefs, v.Capabilities, v.Enabled, v.HealthStatus, v.CreatedAt, v.UpdatedAt}
}

func agentDTO(v store.Agent) AgentDTO {
	return AgentDTO{v.ID, v.ProjectID, v.Name, v.RoleInstructions, v.Engine, v.ModelProfileID, v.EngineSettings, v.ConcurrencyLimit, v.AllowDelegation, v.State, v.CreatedAt, v.UpdatedAt}
}

func runDTO(v store.Run, issueKeys map[string]string) RunDTO {
	return RunDTO{v.ID, v.ProjectID, issueKeyForUUID(issueKeys, v.IssueID), v.WorkspaceID, v.AgentID, v.Attempt, v.Status, v.QueueReason, v.FailureReason, v.CreatedAt, v.StartedAt, v.CompletedAt, v.UpdatedAt, v.CurrentBranch}
}

func delegationDTO(v app.DelegationInspection, issueKeys map[string]string) DelegationDTO {
	var parentRunID, parentAgentID, parentRunStatus *string
	if v.ParentRunID != "" {
		parentRunIDValue, parentAgentIDValue, parentRunStatusValue := v.ParentRunID, v.ParentAgentID, v.ParentRunStatus
		parentRunID, parentAgentID, parentRunStatus = &parentRunIDValue, &parentAgentIDValue, &parentRunStatusValue
	}
	return DelegationDTO{
		ID:                       v.ID,
		ProjectID:                v.ProjectID,
		IssueID:                  issueKeyForUUID(issueKeys, v.IssueID),
		ParentRunID:              parentRunID,
		ParentAgentID:            parentAgentID,
		SourceCommentID:          v.SourceCommentID,
		TargetAgentID:            v.TargetAgentID,
		Task:                     v.Task,
		DelegatedRunID:           v.DelegatedRunID,
		WorkspaceAccess:          store.DelegationWorkspaceAccessWrite,
		RequestKey:               v.RequestKey,
		ParentRunStatus:          parentRunStatus,
		DelegatedRunStatus:       v.DelegatedRunStatus,
		Outcome:                  v.Outcome,
		ResultSummary:            v.ResultSummary,
		ResultEventID:            v.ResultEventID,
		WorkspaceChangesAccepted: v.WorkspaceChangesAccepted,
		WorkspaceRevision:        v.WorkspaceRevision,
		ContinuationJobID:        v.ContinuationJobID,
		CompletedAt:              v.CompletedAt,
		CreatedAt:                v.CreatedAt,
		UpdatedAt:                v.UpdatedAt,
	}
}
