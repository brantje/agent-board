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

type runtimeAcquirer interface {
	Acquire(context.Context, string, string, string) (store.RuntimeInstance, error)
}

func (p *Processor) engineRequest(ctx context.Context, safe executioncontext.SafeContext, launcher *processLauncher, runtimeInstanceID string) (engine.Request, error) {
	request := engine.Request{Context: safe, Launcher: launcher}
	if !store.SupportsQuestionStore(p.store) {
		return request, nil
	}
	questions := any(p.store).(store.QuestionStore)
	request.Questions = &questioner{store: questions, events: p.events, safe: safe, runtimeInstanceID: runtimeInstanceID}
	continuation, err := loadContinuation(ctx, questions, safe)
	if err != nil {
		return engine.Request{}, err
	}
	request.Continuation = continuation
	return request, nil
}

func loadContinuation(ctx context.Context, questions store.QuestionStore, safe executioncontext.SafeContext) (*engine.Continuation, error) {
	runID := safe.Run.ID
	answered, err := questions.ListQuestions(ctx, safe.Project.ID, store.QuestionFilter{RunID: &runID, Statuses: []string{"ANSWERED"}})
	if err != nil {
		return nil, err
	}
	for i := len(answered) - 1; i >= 0; i-- {
		question := answered[i]
		if !question.Blocking {
			continue
		}
		decision, err := questions.GetDecisionByQuestion(ctx, safe.Project.ID, question.ID)
		if err != nil {
			return nil, fmt.Errorf("run execution: answered blocking Question %s has no Decision: %w", question.ID, err)
		}
		if decision.QuestionID == nil || *decision.QuestionID != question.ID || decision.RunID == nil || *decision.RunID != safe.Run.ID {
			return nil, fmt.Errorf("run execution: Decision binding does not match continuation Question")
		}
		var details struct {
			QuestionAnswer store.QuestionAnswer `json:"questionAnswer"`
		}
		if err := json.Unmarshal(decision.SafeDetails, &details); err != nil {
			return nil, fmt.Errorf("run execution: decode Question continuation: %w", err)
		}
		return &engine.Continuation{
			QuestionID: question.ID,
			DecisionID: decision.ID,
			Prompt:     question.Prompt,
			Answer: engine.QuestionAnswer{
				Kind:      details.QuestionAnswer.Kind,
				Text:      details.QuestionAnswer.Text,
				OptionIDs: append([]string(nil), details.QuestionAnswer.OptionIDs...),
			},
		}, nil
	}
	return nil, nil
}

func (p *Processor) acquireRuntime(ctx context.Context, projectID, issueID, runtimeID string) (store.RuntimeInstance, error) {
	if acquirer, ok := p.runtimes.(runtimeAcquirer); ok {
		return acquirer.Acquire(ctx, projectID, issueID, runtimeID)
	}
	return p.runtimes.Create(ctx, projectID, issueID, runtimeID)
}

func (p *Processor) finishWaitingForInput(ctx context.Context, safe executioncontext.SafeContext, instance store.RuntimeInstance) (scheduler.Result, error) {
	if !store.SupportsQuestionStore(p.store) {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(fmt.Errorf("run execution: Question store capability is required for WAITING_FOR_INPUT"), cleanupErr)), nil
	}
	questions := any(p.store).(store.QuestionStore)
	question, err := questions.GetOpenBlockingQuestion(ctx, safe.Project.ID, safe.Run.ID)
	if err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(fmt.Errorf("run execution: persisted blocking Question is required before WAITING_FOR_INPUT: %w", err), cleanupErr)), nil
	}

	cleanupErr := p.cleanupRuntime(ctx, safe, instance)
	if ctx.Err() != nil {
		return scheduler.Result{}, ctx.Err()
	}
	if cleanupErr != nil {
		reason := safeFailure(fmt.Errorf("run execution: cleanup Runtime before waiting: %w", cleanupErr))
		_ = p.record(ctx, safe, "run.failed", map[string]any{"reason": reason}, &instance.ID, nil)
		return scheduler.Result{RunStatus: "FAILED", FailureReason: &reason}, nil
	}
	_ = p.record(ctx, safe, "run.waiting_for_input", map[string]any{"questionId": question.ID}, &instance.ID, nil)
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
