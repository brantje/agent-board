package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type noopRuntimeAcquisitionLock struct{}

func (noopRuntimeAcquisitionLock) Release() error { return nil }

func (s *runtimeServiceStore) AcquireRuntimeAcquisitionLock(context.Context, string, string) (store.RuntimeAcquisitionLock, error) {
	return noopRuntimeAcquisitionLock{}, nil
}
