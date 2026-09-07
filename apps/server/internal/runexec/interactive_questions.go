package runexec

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	interactiveQuestionPollInitialInterval = 200 * time.Millisecond
	// Keep answer latency bounded while preventing long-lived waiting runs from
	// polling PostgreSQL at the initial five queries per second indefinitely.
	interactiveQuestionPollMaxInterval = 2 * time.Second
)

type interactiveQuestioner struct {
	store             store.QuestionStore
	interactive       store.InteractiveQuestionStore
	events            *evidence.Recorder
	safe              executioncontext.SafeContext
	runtimeInstanceID string
	engine            string
}

func (q *interactiveQuestioner) Open(ctx context.Context, correlationKey string, request engine.QuestionRequest) (engine.Question, error) {
	if q == nil || q.store == nil || q.interactive == nil || q.events == nil {
		return engine.Question{}, fmt.Errorf("run execution: interactive Question capability is unavailable")
	}
	command, _, err := q.prepareOpenCommand(correlationKey, request)
	if err != nil {
		return engine.Question{}, err
	}
	result, err := q.interactive.OpenInteractiveQuestion(ctx, command)
	if err != nil {
		return engine.Question{}, err
	}
	if err := q.recordOpenResult(ctx, result); err != nil {
		return engine.Question{}, err
	}
	return engine.Question{ID: result.Question.ID, Blocking: true}, nil
}

func (q *interactiveQuestioner) OpenBatch(ctx context.Context, requests []engine.CorrelatedQuestionRequest) ([]engine.Question, error) {
	if q == nil || q.store == nil || q.interactive == nil || q.events == nil {
		return nil, fmt.Errorf("run execution: interactive Question capability is unavailable")
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("run execution: interactive Question batch is empty")
	}
	batchStore, ok := any(q.interactive).(store.InteractiveQuestionBatchStore)
	if !ok {
		return nil, fmt.Errorf("run execution: interactive Question batch capability is unavailable")
	}

	commands := make([]store.OpenInteractiveQuestionCommand, len(requests))
	for index, request := range requests {
		command, _, err := q.prepareOpenCommand(request.CorrelationKey, request.Question)
		if err != nil {
			return nil, fmt.Errorf("run execution: prepare interactive Question batch item %d: %w", index, err)
		}
		commands[index] = command
	}

	batch, err := batchStore.OpenInteractiveQuestions(ctx, commands)
	if err != nil {
		return nil, err
	}
	if len(batch.Questions) != len(requests) {
		return nil, fmt.Errorf("run execution: interactive Question batch returned %d Questions for %d requests", len(batch.Questions), len(requests))
	}

	opened := make([]engine.Question, len(batch.Questions))
	for index, result := range batch.Questions {
		if result.Created {
			if err := q.recordCreated(ctx, result.Question); err != nil {
				return nil, err
			}
		}
		opened[index] = engine.Question{ID: result.Question.ID, Blocking: true}
	}
	if batch.EnteredWaiting {
		questionID := batch.Questions[0].Question.ID
		for _, result := range batch.Questions {
			if result.EnteredWaiting {
				questionID = result.Question.ID
				break
			}
		}
		if err := q.record(ctx, "run.waiting_for_input", map[string]any{"questionId": questionID}); err != nil {
			return nil, err
		}
	}
	return opened, nil
}

func (q *interactiveQuestioner) prepareOpenCommand(correlationKey string, request engine.QuestionRequest) (store.OpenInteractiveQuestionCommand, []store.QuestionOption, error) {
	if err := validateQuestionRequest(request); err != nil {
		return store.OpenInteractiveQuestionCommand{}, nil, err
	}
	if !request.Blocking {
		return store.OpenInteractiveQuestionCommand{}, nil, fmt.Errorf("run execution: interactive Questions must be blocking")
	}
	if correlationKey == "" {
		return store.OpenInteractiveQuestionCommand{}, nil, fmt.Errorf("run execution: interactive Question correlation key is required")
	}

	options := make([]store.QuestionOption, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, store.QuestionOption{ID: option.ID, Label: option.Label})
	}
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return store.OpenInteractiveQuestionCommand{}, nil, err
	}
	return store.OpenInteractiveQuestionCommand{
		Question: store.Question{
			ProjectID:      q.safe.Project.ID,
			IssueID:        q.safe.Issue.ID,
			RunID:          q.safe.Run.ID,
			Prompt:         request.Prompt,
			Kind:           request.Kind,
			Options:        encodedOptions,
			Recommendation: request.Recommendation,
			Blocking:       true,
			Status:         "OPEN",
		},
		Engine:         q.engine,
		CorrelationKey: correlationKey,
	}, options, nil
}

func (q *interactiveQuestioner) recordOpenResult(ctx context.Context, result store.OpenInteractiveQuestionResult) error {
	if result.Created {
		if err := q.recordCreated(ctx, result.Question); err != nil {
			return err
		}
	}
	if result.EnteredWaiting {
		return q.record(ctx, "run.waiting_for_input", map[string]any{"questionId": result.Question.ID})
	}
	return nil
}

func (q *interactiveQuestioner) recordCreated(ctx context.Context, question store.Question) error {
	options := make([]store.QuestionOption, 0)
	if len(question.Options) != 0 {
		if err := json.Unmarshal(question.Options, &options); err != nil {
			return fmt.Errorf("run execution: decode persisted Question options: %w", err)
		}
	}
	return q.record(ctx, "question.created", map[string]any{
		"questionId": question.ID,
		"prompt":     question.Prompt,
		"kind":       question.Kind,
		"options":    options,
		"blocking":   true,
	})
}

func (q *interactiveQuestioner) WaitAnswer(ctx context.Context, questionID string) (engine.QuestionAnswer, error) {
	if q == nil || q.store == nil {
		return engine.QuestionAnswer{}, fmt.Errorf("run execution: interactive Question capability is unavailable")
	}
	if questionID == "" {
		return engine.QuestionAnswer{}, fmt.Errorf("run execution: interactive Question id is required")
	}

	pollInterval := interactiveQuestionPollInitialInterval
	for {
		question, err := q.store.GetQuestion(ctx, q.safe.Project.ID, questionID)
		if err != nil {
			return engine.QuestionAnswer{}, err
		}
		switch question.Status {
		case "ANSWERED":
			decision, err := q.store.GetDecisionByQuestion(ctx, q.safe.Project.ID, questionID)
			if err != nil {
				return engine.QuestionAnswer{}, err
			}
			var details struct {
				QuestionAnswer store.QuestionAnswer `json:"questionAnswer"`
			}
			if err := json.Unmarshal(decision.SafeDetails, &details); err != nil {
				return engine.QuestionAnswer{}, fmt.Errorf("run execution: decode interactive Question answer: %w", err)
			}
			return engine.QuestionAnswer{
				Kind:      details.QuestionAnswer.Kind,
				Text:      details.QuestionAnswer.Text,
				OptionIDs: append([]string(nil), details.QuestionAnswer.OptionIDs...),
			}, nil
		case "CANCELLED":
			return engine.QuestionAnswer{}, fmt.Errorf("run execution: interactive Question was cancelled")
		case "OPEN":
		default:
			return engine.QuestionAnswer{}, fmt.Errorf("run execution: unexpected interactive Question status %q", question.Status)
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return engine.QuestionAnswer{}, ctx.Err()
		case <-timer.C:
		}
		pollInterval = nextInteractiveQuestionPollInterval(pollInterval)
	}
}

func nextInteractiveQuestionPollInterval(current time.Duration) time.Duration {
	if current >= interactiveQuestionPollMaxInterval {
		return interactiveQuestionPollMaxInterval
	}
	next := current * 2
	if next > interactiveQuestionPollMaxInterval {
		return interactiveQuestionPollMaxInterval
	}
	return next
}

func (q *interactiveQuestioner) Resolve(ctx context.Context, questionID string) error {
	if q == nil || q.interactive == nil {
		return fmt.Errorf("run execution: interactive Question capability is unavailable")
	}
	result, err := q.interactive.ResolveInteractiveQuestion(ctx, q.safe.Project.ID, questionID)
	if err != nil {
		return err
	}
	if result.Resumed {
		return q.record(ctx, "run.resumed", map[string]any{"questionId": questionID})
	}
	return nil
}

func (q *interactiveQuestioner) record(ctx context.Context, eventType string, payload any) error {
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return err
	}
	issueID, runID, agentID, workspaceID, runtimeID := q.safe.Issue.ID, q.safe.Run.ID, q.safe.Agent.ID, q.safe.Workspace.ID, q.runtimeInstanceID
	_, err = q.events.Record(ctx, store.Event{
		Type:              eventType,
		ProjectID:         q.safe.Project.ID,
		IssueID:           &issueID,
		RunID:             &runID,
		AgentID:           &agentID,
		WorkspaceID:       &workspaceID,
		RuntimeInstanceID: &runtimeID,
		Actor:             store.EmptyObject,
		Payload:           encoded,
	})
	return err
}

var _ engine.InteractiveQuestioner = (*interactiveQuestioner)(nil)
var _ engine.InteractiveQuestionBatcher = (*interactiveQuestioner)(nil)
