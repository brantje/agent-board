package evidence

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) UpdateIssuePatchMutation(ctx context.Context, patch store.IssuePatch, actor json.RawMessage) (store.IssueMutationResult, error) {
	mutationStore, ok := s.ControlPlaneStore.(store.IssuePatchMutationStore)
	if !ok {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	return mutationStore.UpdateIssuePatchMutation(ctx, patch, actor)
}
