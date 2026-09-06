package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type questioner struct {
	store             store.QuestionStore
	events            *evidence.Recorder
	safe              executioncontext.SafeContext
	runtimeInstanceID string
}

func (p *Processor) engineRequest(safe executioncontext.SafeContext, launcher *processLauncher, runtimeInstanceID string) engine.Request {
	request := engine.Request{Context: safe, Launcher: launcher}
	questions, ok := any(p.store).(store.QuestionStore)
	if ok {
		request.Questions = &questioner{store: questions, events: p.events, safe: safe, runtimeInstanceID: runtimeInstanceID}
	}
	return request
}

func (p *Processor) acquireRuntime(ctx context.Context, projectID, issueID, runtimeID string) (store.RuntimeInstance, error) {
	return p.runtimes.Create(ctx, projectID, issueID, runtimeID)
}

func (p *Processor) finishWaitingForInput(ctx context.Context, safe executioncontext.SafeContext, instance store.RuntimeInstance) (scheduler.Result, error) {
	questions, ok := any(p.store).(store.QuestionStore)
	if !ok {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(fmt.Errorf("run execution: Question store capability is required for WAITING_FOR_INPUT"), cleanupErr)), nil
	}
	question, err := questions.GetOpenBlockingQuestion(ctx, safe.Project.ID, safe.Run.ID)
	if err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(fmt.Errorf("run execution: persisted blocking Question is required before WAITING_FOR_INPUT: %w", err), cleanupErr)), nil
	}
	_ = p.record(ctx, safe, "run.waiting_for_input", map[string]any{"questionId": question.ID}, &instance.ID, nil)
	_ = p.cleanupRuntime(ctx, safe, instance)
	if ctx.Err() != nil {
		return scheduler.Result{}, ctx.Err()
	}
	return scheduler.Result{RunStatus: "WAITING_FOR_INPUT"}, nil
}

func (q *questioner) Ask(ctx context.Context, request engine.QuestionRequest) (engine.Question, error) {
	if q == nil || q.store == nil || q.events == nil {
		return engine.Question{}, fmt.Errorf("run execution: Question capability is unavailable")
	}
	if err := validateQuestionRequest(request); err != nil {
		return engine.Question{}, err
	}
	if request.Blocking {
		existing, err := q.store.GetOpenBlockingQuestion(ctx, q.safe.Project.ID, q.safe.Run.ID)
		switch {
		case err == nil:
			return engine.Question{ID: existing.ID, Blocking: true}, engine.ErrWaitingForInput
		case !errors.Is(err, store.ErrNotFound):
			return engine.Question{}, err
		}
	}

	options := make([]store.QuestionOption, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, store.QuestionOption{ID: option.ID, Label: option.Label})
	}
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return engine.Question{}, err
	}
	question, err := q.store.CreateQuestion(ctx, store.Question{
		ProjectID:      q.safe.Project.ID,
		IssueID:        q.safe.Issue.ID,
		RunID:          q.safe.Run.ID,
		Prompt:         request.Prompt,
		Kind:           request.Kind,
		Options:        encodedOptions,
		Recommendation: request.Recommendation,
		Blocking:       request.Blocking,
		Status:         "OPEN",
	})
	if err != nil {
		return engine.Question{}, err
	}

	payload, err := evidence.EncodePayload(map[string]any{
		"questionId": question.ID,
		"prompt":     question.Prompt,
		"kind":       question.Kind,
		"options":    options,
		"blocking":   question.Blocking,
	})
	if err == nil {
		issueID, runID, agentID, workspaceID, runtimeID := q.safe.Issue.ID, q.safe.Run.ID, q.safe.Agent.ID, q.safe.Workspace.ID, q.runtimeInstanceID
		_, err = q.events.Record(ctx, store.Event{
			Type:              "question.created",
			ProjectID:         q.safe.Project.ID,
			IssueID:           &issueID,
			RunID:             &runID,
			AgentID:           &agentID,
			WorkspaceID:       &workspaceID,
			RuntimeInstanceID: &runtimeID,
			Actor:             store.EmptyObject,
			Payload:           payload,
		})
	}
	result := engine.Question{ID: question.ID, Blocking: question.Blocking}
	if request.Blocking {
		return result, errors.Join(engine.ErrWaitingForInput, err)
	}
	return result, err
}

func validateQuestionRequest(request engine.QuestionRequest) error {
	if strings.TrimSpace(request.Prompt) == "" {
		return fmt.Errorf("run execution: Question prompt is required")
	}
	switch request.Kind {
	case "TEXT":
		if len(request.Options) != 0 {
			return fmt.Errorf("run execution: TEXT Questions cannot define options")
		}
	case "SINGLE_CHOICE", "MULTI_CHOICE":
		if len(request.Options) == 0 {
			return fmt.Errorf("run execution: choice Questions require options")
		}
		seen := make(map[string]struct{}, len(request.Options))
		for _, option := range request.Options {
			if strings.TrimSpace(option.ID) == "" || strings.TrimSpace(option.Label) == "" {
				return fmt.Errorf("run execution: Question options require id and label")
			}
			if _, duplicate := seen[option.ID]; duplicate {
				return fmt.Errorf("run execution: Question option ids must be unique")
			}
			seen[option.ID] = struct{}{}
		}
	default:
		return fmt.Errorf("run execution: unsupported Question kind %q", request.Kind)
	}
	return nil
}
