package runexec

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

var engineActivityTypes = map[string]struct{}{
	"agent.message":   {},
	"tool.started":    {},
	"tool.completed":  {},
	"tool.failed":     {},
	"test.started":    {},
	"test.completed":  {},
	"test.failed":     {},
	"file.created":    {},
	"file.modified":   {},
	"file.deleted":    {},
	"file.renamed":    {},
}

func (l *processLauncher) RecordActivity(ctx context.Context, activity engine.ActivityEvent) error {
	if l == nil {
		return fmt.Errorf("run execution: activity sink is unavailable")
	}
	if _, allowed := engineActivityTypes[activity.Type]; !allowed {
		return fmt.Errorf("run execution: unsupported engine activity type %q", activity.Type)
	}
	_, err := l.record(ctx, activity.Type, activity.Payload, nil)
	return err
}

var _ engine.ActivitySink = (*processLauncher)(nil)
