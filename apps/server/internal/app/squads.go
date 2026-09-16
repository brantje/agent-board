package app

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) squadStore() (store.SquadStore, error) {
	squads, ok := s.store.(store.SquadStore)
	if !ok {
		return nil, NewError("squad_management_unavailable", "squad management is unavailable", store.ErrInvalidArgument)
	}
	return squads, nil
}

func (s *Service) ListSquads(ctx context.Context, projectID string) ([]store.Squad, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
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
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return store.Squad{}, err
	}
	if !validSquadUUID(squadID) {
		return store.Squad{}, invalid("squad id must be a UUID")
	}
	squads, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	value, err := squads.GetSquad(ctx, projectID, squadID)
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
	squads, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	value, err := squads.CreateSquad(ctx, prepared)
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
	squads, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	value, err := squads.UpdateSquad(ctx, prepared)
	return value, translateStoreError(err, "squad")
}

func (s *Service) DeleteSquad(ctx context.Context, projectID, squadID string) error {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return err
	}
	if !validSquadUUID(squadID) {
		return invalid("squad id must be a UUID")
	}
	squads, err := s.squadStore()
	if err != nil {
		return err
	}
	return translateStoreError(squads.DeleteSquad(ctx, projectID, squadID), "squad")
}

func (s *Service) prepareSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return store.Squad{}, invalid("squad name is required")
	}
	if !validSquadUUID(input.LeaderAgentID) {
		return store.Squad{}, invalid("squad leader agent id must be a UUID")
	}

	seen := map[string]struct{}{input.LeaderAgentID: {}}
	members := make([]store.SquadMember, 0, len(input.Members))
	for _, member := range input.Members {
		if !validSquadUUID(member.AgentID) {
			return store.Squad{}, invalid("squad member agent id must be a UUID")
		}
		if _, exists := seen[member.AgentID]; exists {
			return store.Squad{}, invalid("squad Agents must have one canonical membership representation")
		}
		seen[member.AgentID] = struct{}{}

		var role *string
		if member.Role != nil {
			normalized := strings.TrimSpace(*member.Role)
			if normalized != "" {
				role = &normalized
			}
		}
		members = append(members, store.SquadMember{AgentID: member.AgentID, Role: role})
	}
	input.Members = members

	if err := s.requireUsableSquadAgent(ctx, input.ProjectID, input.LeaderAgentID); err != nil {
		return store.Squad{}, err
	}
	for _, member := range input.Members {
		if err := s.requireUsableSquadAgent(ctx, input.ProjectID, member.AgentID); err != nil {
			return store.Squad{}, err
		}
	}
	return input, nil
}

func (s *Service) requireUsableSquadAgent(ctx context.Context, projectID, agentID string) error {
	agent, err := s.GetAgent(ctx, &projectID, agentID)
	if err != nil {
		return err
	}
	if agent.State != "ENABLED" {
		return invalid("squad leader and members must be enabled Agents")
	}
	return nil
}

func validSquadUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(value) == nil && id.Valid
}
