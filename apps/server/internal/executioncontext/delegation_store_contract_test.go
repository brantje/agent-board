package executioncontext

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// fakeStore models an ordinary Run unless a focused test overrides this method.
func (f fakeStore) GetDelegationByRun(context.Context, string, string) (store.Delegation, error) {
	return store.Delegation{}, store.ErrNotFound
}
