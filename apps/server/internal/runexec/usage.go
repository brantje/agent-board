package runexec

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/modelusage"
)

func (l *processLauncher) RecordModelUsage(ctx context.Context, sample modelusage.Sample) error {
	if l == nil {
		return fmt.Errorf("run execution: usage sink is unavailable")
	}
	if err := sample.Validate(); err != nil {
		return fmt.Errorf("run execution: invalid model usage: %w", err)
	}
	_, err := l.record(ctx, "model.usage", sample, nil)
	return err
}

var _ engine.UsageSink = (*processLauncher)(nil)
