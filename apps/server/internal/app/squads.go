package app

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) ListSquads(ctx context.Context, projectID string) ([]store.Squad, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	values, err := s.store.ListSquads(ctx, projectID)
	return values, translateStoreError(err, "squad")
}

func (s *Service) GetSquad(ctx context.Context, projectID, squadID string) (store.Squad, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return store.Squad{}, err
	}
	if !validSquadUUID(squadID) {
		return store.Squad{}, invalid("squad id must be a UUID")
	}
	value, err := s.store.GetSquad(ctx, projectID, squadID)
	return value, translateStoreError(err, "squad")
}

func (s *Service) CreateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Squad{}, err
	}
	prepared, err := s.prepareSquad(ctx, input)
	if err != nil {
		return store.Squad{}, err
	}
	value, err := s.store.CreateSquad(ctx, prepared)
	return value, translateStoreError(err, "squad")
}

func (s *Service) UpdateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Squad{}, err
	}
	if !validSquadUUID(input.ID) {
		return store.Squad{}, invalid("squad id must be a UUID")
	}
	prepared, err := s.prepareSquad(ctx, input)
	if err != nil {
		return store.Squad{}, err
	}
	result, err := s.store.UpdateSquad(ctx, prepared)
	if err != nil {
		return store.Squad{}, translateStoreError(err, "squad")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	if result.LeaderChanged {
		s.reconcileExecutionConfiguration(ctx, store.IssueExecutionFilter{
			ProjectID: result.Squad.ProjectID,
			AgentID:   result.Squad.LeaderAgentID,
			SquadID:   result.Squad.ID,
		})
	}
	return result.Squad, nil
}

func (s *Service) DeleteSquad(ctx context.Context, projectID, squadID string) error {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return err
	}
	if !validSquadUUID(squadID) {
		return invalid("squad id must be a UUID")
	}
	return translateStoreError(s.store.DeleteSquad(ctx, projectID, squadID), "squad")
}

func (s *Service) prepareSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return store.Squad{}, invalid("squad name is required")
	}
	if !validSquadUUID(input.LeaderAgentID) {
		return store.Squad{}, invalid("squad leader agent id must be a UUID")
	}

	seen := map[string]struct{}{typedSquadMemberKey(store.SquadMemberTypeAgent, input.LeaderAgentID): {}}
	members := make([]store.SquadMember, 0, len(input.Members))
	for _, member := range input.Members {
		if member.Type != store.SquadMemberTypeAgent && member.Type != store.SquadMemberTypeUser {
			return store.Squad{}, invalid("squad member type must be AGENT or USER")
		}
		if !validSquadUUID(member.ID) {
			return store.Squad{}, invalid("squad member id must be a UUID")
		}
		key := typedSquadMemberKey(member.Type, member.ID)
		if _, exists := seen[key]; exists {
			return store.Squad{}, invalid("squad members must have one canonical typed identity representation")
		}
		seen[key] = struct{}{}

		var role *string
		if member.Role != nil {
			normalized := strings.TrimSpace(*member.Role)
			if normalized != "" {
				role = &normalized
			}
		}
		members = append(members, store.SquadMember{Type: member.Type, ID: member.ID, Role: role})
	}
	input.Members = members

	if err := s.requireUsableSquadAgent(ctx, input.ProjectID, input.LeaderAgentID); err != nil {
		return store.Squad{}, err
	}
	for _, member := range input.Members {
		switch member.Type {
		case store.SquadMemberTypeAgent:
			if err := s.requireUsableSquadAgent(ctx, input.ProjectID, member.ID); err != nil {
				return store.Squad{}, err
			}
		case store.SquadMemberTypeUser:
			if err := s.requireUsableSquadUser(ctx, input.ProjectID, member.ID); err != nil {
				return store.Squad{}, err
			}
		}
	}
	return input, nil
}

func typedSquadMemberKey(memberType, id string) string {
	return memberType + ":" + id
}

func (s *Service) requireUsableSquadAgent(ctx context.Context, projectID, agentID string) error {
	agent, err := s.GetAgent(ctx, &projectID, agentID)
	if err != nil {
		return err
	}
	if agent.State != "ENABLED" {
		return invalid("squad leader and Agent members must be enabled Agents")
	}
	return nil
}

func (s *Service) requireUsableSquadUser(ctx context.Context, projectID, userID string) error {
	var eligibility store.ProjectWorkflowUserEligibilityStore
	if candidate, ok := s.store.(store.ProjectWorkflowUserEligibilityStore); ok {
		eligibility = candidate
	} else if candidate, ok := s.assignmentStore.(store.ProjectWorkflowUserEligibilityStore); ok {
		// Runtime services intentionally keep the evidence/redaction store narrow;
		// assignmentStore is already rebound to the authoritative base store.
		eligibility = candidate
	}
	if eligibility == nil {
		return invalid("squad User membership validation is unavailable")
	}
	if err := eligibility.ValidateProjectWorkflowUser(ctx, projectID, userID); err != nil {
		if errors.Is(err, store.ErrInvalidArgument) || errors.Is(err, store.ErrNotFound) {
			return invalid("squad User members must be active Project members or administrators")
		}
		return err
	}
	return nil
}

func validSquadUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(value) == nil && id.Valid
}
