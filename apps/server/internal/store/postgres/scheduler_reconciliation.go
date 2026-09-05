package postgres

import (
	"context"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) claimExpiredJobForReconciliation(context.Context, string, time.Duration) (*store.SchedulerAdmission, error) {
	return nil, store.ErrInvalidArgument
}

func (s *Store) resolveReconciliation(context.Context, store.SchedulerReconciliation) (store.Run, error) {
	return store.Run{}, store.ErrInvalidArgument
}
