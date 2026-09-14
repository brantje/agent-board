package evidence

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) TransitionAdmittedJobMutation(ctx context.Context, input store.SchedulerTransition) (store.SchedulerMutationResult, error) {
	if input.FailureReason != nil {
		value := s.registry.RedactString(input.RunID, *input.FailureReason)
		input.FailureReason = &value
	}
	base, ok := s.ControlPlaneStore.(store.SchedulerMutationStore)
	if !ok {
		run, err := s.ControlPlaneStore.TransitionAdmittedJob(ctx, input)
		return store.SchedulerMutationResult{Run: run}, err
	}
	return base.TransitionAdmittedJobMutation(ctx, input)
}

func (s *RedactingStore) ResolveReconciliationMutation(ctx context.Context, input store.SchedulerReconciliation) (store.SchedulerMutationResult, error) {
	base, ok := s.ControlPlaneStore.(store.SchedulerMutationStore)
	if !ok {
		run, err := s.ControlPlaneStore.ResolveReconciliation(ctx, input)
		return store.SchedulerMutationResult{Run: run}, err
	}
	return base.ResolveReconciliationMutation(ctx, input)
}

var _ store.SchedulerMutationStore = (*RedactingStore)(nil)
