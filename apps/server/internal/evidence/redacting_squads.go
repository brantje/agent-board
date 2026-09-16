package evidence

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) squadStore() (store.SquadStore, error) {
	base, ok := s.ControlPlaneStore.(store.SquadStore)
	if !ok {
		return nil, fmt.Errorf("redacting store base does not support squads")
	}
	return base, nil
}

func (s *RedactingStore) CreateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	base, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	return base.CreateSquad(ctx, input)
}

func (s *RedactingStore) GetSquad(ctx context.Context, projectID, squadID string) (store.Squad, error) {
	base, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	return base.GetSquad(ctx, projectID, squadID)
}

func (s *RedactingStore) ListSquads(ctx context.Context, projectID string) ([]store.Squad, error) {
	base, err := s.squadStore()
	if err != nil {
		return nil, err
	}
	return base.ListSquads(ctx, projectID)
}

func (s *RedactingStore) UpdateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	base, err := s.squadStore()
	if err != nil {
		return store.Squad{}, err
	}
	return base.UpdateSquad(ctx, input)
}

func (s *RedactingStore) DeleteSquad(ctx context.Context, projectID, squadID string) error {
	base, err := s.squadStore()
	if err != nil {
		return err
	}
	return base.DeleteSquad(ctx, projectID, squadID)
}

var _ store.SquadStore = (*RedactingStore)(nil)
