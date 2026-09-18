package runexec

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

var engineActivityTypes = map[string]struct{}{
	"engine.execution.completed": {},
	"agent.message":               {},
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
	if err != nil {
		return err
	}
	if shouldObserveBranchAfterActivity(activity.Type) {
		l.branches.observeIfChanged(ctx, l.safe, &l.runtimeInstanceID)
	}
	return nil
}

var _ engine.ActivitySink = (*processLauncher)(nil)

type executionAdmissionPromptService interface {
	GetOrCreateAdmissionPrompt(context.Context, string, string, string) (string, error)
}

func (l *processLauncher) GetOrCreateAdmissionPrompt(ctx context.Context, executionSessionID, prompt string) (string, error) {
	if l == nil || l.sessions == nil {
		return "", fmt.Errorf("run execution: Execution Session admission identity is unavailable")
	}
	service, ok := l.sessions.(executionAdmissionPromptService)
	if !ok {
		return "", fmt.Errorf("run execution: Execution Session admission identity is unsupported")
	}
	return service.GetOrCreateAdmissionPrompt(ctx, l.scope.ProjectID, executionSessionID, prompt)
}

var _ engine.ExecutionAdmissionPromptStore = (*processLauncher)(nil)
