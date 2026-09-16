package app

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Service) squadStore() (store.SquadStore, error) {
	squads, ok := s.store.(store.SquadStore)
	if !ok {
		return nil, NewError("squad_management_unavailable", "squad management is unavailable", store.ErrInvalidArgument)
	}
	return squads, nil
}

func normalizeSquad(input store.Squad) (store.Squad, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.LeaderAgentID = strings.TrimSpace(input.LeaderAgentID)
	if input.ProjectID == "" {
		return store.Squad{}, invalid("project id is required")
	}
	if input.Name == "" {
		return store.Squad{}, invalid("squad name is required")
	}
	if input.LeaderAgentID == "" {
		return store.Squad{}, invalid("squad leader agent is required")
	}

	seen := make(map[string]struct{}, len(input.Members))
	members := make([]store.SquadMember, 0, len(input.Members))
	for _, member := range input.Members {
		member.AgentID = strings.TrimSpace(member.AgentID)
		if member.AgentID == "" {
			return store.Squad{}, invalid("squad member agent id is required")
		}
		if member.AgentID == input.LeaderAgentID {
			return store.Squad{}, invalid("squad leader cannot also be a member")
		}
		if _, exists := seen[member.AgentID]; exists {
			return store.Squad{}, invalid("squad member agents must be unique")
		}
		seen[member.AgentID] = struct{}{}
		if member.Role != nil {
			role := strings.TrimSpace(*member.Role)
			if role == "" {
				member.Role = nil
			} else {
				member.Role = &role
			}
		}
		members = append(members, member)
	}
	input.Members = members
	return input, nil
}

func (s *Service) validateSquadAgent(ctx context.Context, projectID, agentID string, requireEnabled bool) error {
	agent, err := s.GetAgent(ctx, &projectID, agentID)
	if err != nil {
		return err
	}
	if requireEnabled && agent.State != "ENABLED" {
		return NewError("agent_unavailable", "squad agents must be enabled when selected", store.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) validateSquadAgents(ctx context.Context, input store.Squad, current *store.Squad) error {
	requireLeaderEnabled := current == nil || current.LeaderAgentID != input.LeaderAgentID
	if err := s.validateSquadAgent(ctx, input.ProjectID, input.LeaderAgentID, requireLeaderEnabled); err != nil {
		return err
	}

	currentMembers := make(map[string]struct{})
	if current != nil {
		for _, member := range current.Members {
			currentMembers[member.AgentID] = struct{}{}
		}
	}
	for _, member := range input.Members {
		_, alreadyMember := currentMembers[member.AgentID]
		if err := s.validateSquadAgent(ctx, input.ProjectID, member.AgentID, current == nil || !alreadyMember); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListSquads(ctx context.Context, projectID string) ([]store.Squad, error) {
	projectID = strings.TrimSpace(projectID)
	if err := s.ensureScope(ctx, &projectID); err != nil {
		return nil, err
	}
	squads, err := s.squadStore()
	if err != nil {
		return nil, err
	}
	values, err := squads.ListSquads(ctx, projectID)
	return values, translateStoreError(err, "squad")
}

func (s *Service) GetSquad(ctx context.Context, projectID, squadID string) (store.Squad, error) {
	projectID = strings.TrimSpace(projectID)
	squadID = strings.TrimSpace(squadID)
	if err := s.ensureScope(ctx, &projectID); err != nil {
		return store.Squad{}, err
	}
	squads, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	value, err := squads.GetSquad(ctx, projectID, squadID)
	return value, translateStoreError(err, "squad")
}

func (s *Service) CreateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	input, err := normalizeSquad(input)
	if err != nil {
		return store.Squad{}, err
	}
	if err := s.ensureScope(ctx, &input.ProjectID); err != nil {
		return store.Squad{}, err
	}
	if err := s.validateSquadAgents(ctx, input, nil); err != nil {
		return store.Squad{}, err
	}
	squads, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	value, err := squads.CreateSquad(ctx, input)
	return value, translateStoreError(err, "squad")
}

func (s *Service) UpdateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	input, err := normalizeSquad(input)
	if err != nil {
		return store.Squad{}, err
	}
	if input.ID == "" {
		return store.Squad{}, invalid("squad id is required")
	}
	if err := s.ensureScope(ctx, &input.ProjectID); err != nil {
		return store.Squad{}, err
	}
	squads, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	current, err := squads.GetSquad(ctx, input.ProjectID, input.ID)
	if err != nil {
		return store.Squad{}, translateStoreError(err, "squad")
	}
	if err := s.validateSquadAgents(ctx, input, &current); err != nil {
		return store.Squad{}, err
	}
	value, err := squads.UpdateSquad(ctx, input)
	return value, translateStoreError(err, "squad")
}

func (s *Service) DeleteSquad(ctx context.Context, projectID, squadID string) error {
	projectID = strings.TrimSpace(projectID)
	squadID = strings.TrimSpace(squadID)
	if err := s.ensureScope(ctx, &projectID); err != nil {
		return err
	}
	squads, err := s.squadStore()
	if err != nil {
		return err
	}
	return translateStoreError(squads.DeleteSquad(ctx, projectID, squadID), "squad")
}
