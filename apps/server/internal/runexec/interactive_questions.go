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
	interactiveQuestionPollMaxInterval    = 2 * time.Second
	interactiveQuestionReplyAcceptedEvent = "engine.question_reply_accepted"
	interactiveQuestionResolvedEvent      = "engine.question_binding_resolved"
	interactiveQuestionEventPageSize      = 500
)

type runEventReader interface {
	ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error)
}

type interactiveQuestioner struct {
	store       store.QuestionStore
	interactive store.InteractiveQuestionStore
	events      *evidence.Recorder
	eventReader runEventReader
	safe        executioncontext.SafeContext
	engine      string
}

func (q *interactiveQuestioner) Open(ctx context.Context, correlationKey string, request engine.QuestionRequest) (engine.Question, error) {
	if q == nil || q.store == nil || q.interactive == nil || q.events == nil {
		return engine.Question{}, fmt.Errorf("run execution: interactive Question capability is unavailable")
	}
	command, err := q.prepareOpenCommand(correlationKey, request)
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
	return engine.Question{ID: result.Question.ID, Blocking: true, Custom: result.Question.Custom}, nil
}

func (q *interactiveQuestioner) OpenBatch(ctx context.Context, requests []engine.CorrelatedQuestionRequest) ([]engine.Question, error) {
	if q == nil || q.store == nil || q.interactive == nil || q.events == nil {
		return nil, fmt.Errorf("run execution: interactive Question capability is unavailable")
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("run execution: interactive Question batch is empty")
	}
	batchStore, ok := any(q.interactive).(store.InteractiveQuestionBatchStore)
	if !ok || !store.SupportsInteractiveQuestionBatchStore(q.interactive) {
		if len(requests) == 1 {
			opened, err := q.Open(ctx, requests[0].CorrelationKey, requests[0].Question)
			if err != nil {
				return nil, err
			}
			return []engine.Question{opened}, nil
		}
		return nil, fmt.Errorf("run execution: interactive Question batch capability is unavailable")
	}

	commands := make([]store.OpenInteractiveQuestionCommand, len(requests))
	for index, request := range requests {
		command, err := q.prepareOpenCommand(request.CorrelationKey, request.Question)
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

	// Stores that return durable lifecycle events own canonical evidence
	// delivery. Only use the recorder as a compatibility fallback for stores
	// that do not persist those events themselves.
	storeOwnsEvidence := len(batch.Events) != 0
	if storeOwnsEvidence {
		q.publishPersisted(ctx, batch.Events)
	}
	opened := make([]engine.Question, len(batch.Questions))
	for index, result := range batch.Questions {
		if result.Created && !storeOwnsEvidence {
			if err := q.recordCreated(ctx, result.Question); err != nil {
				return nil, err
			}
		}
		opened[index] = engine.Question{ID: result.Question.ID, Blocking: true, Custom: result.Question.Custom}
	}
	if batch.EnteredWaiting && !storeOwnsEvidence {
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

func (q *interactiveQuestioner) prepareOpenCommand(correlationKey string, request engine.QuestionRequest) (store.OpenInteractiveQuestionCommand, error) {
	if err := validateQuestionRequest(request); err != nil {
		return store.OpenInteractiveQuestionCommand{}, err
	}
	if !request.Blocking {
		return store.OpenInteractiveQuestionCommand{}, fmt.Errorf("run execution: interactive Questions must be blocking")
	}
	if correlationKey == "" {
		return store.OpenInteractiveQuestionCommand{}, fmt.Errorf("run execution: interactive Question correlation key is required")
	}

	options := make([]store.QuestionOption, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, store.QuestionOption{ID: option.ID, Label: option.Label})
	}
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return store.OpenInteractiveQuestionCommand{}, err
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
			Custom:         request.Custom,
			Blocking:       true,
			Status:         "OPEN",
		},
		Engine:         q.engine,
		CorrelationKey: correlationKey,
	}, nil
}

func (q *interactiveQuestioner) recordOpenResult(ctx context.Context, result store.OpenInteractiveQuestionResult) error {
	if len(result.Events) != 0 {
		q.publishPersisted(ctx, result.Events)
		return nil
	}
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

func (q *interactiveQuestioner) MarkReplyAccepted(ctx context.Context, accepted []engine.AcceptedInteractiveQuestionReply) error {
	if q == nil || q.events == nil || q.eventReader == nil || q.engine == "" {
		return fmt.Errorf("run execution: interactive Question reply tracking is unavailable")
	}
	if len(accepted) == 0 {
		return fmt.Errorf("run execution: accepted interactive Question reply is empty")
	}
	seen := make(map[string]struct{}, len(accepted))
	for _, binding := range accepted {
		if binding.QuestionID == "" || binding.CorrelationKey == "" {
			return fmt.Errorf("run execution: accepted interactive Question reply requires question id and correlation key")
		}
		if _, duplicate := seen[binding.QuestionID]; duplicate {
			return fmt.Errorf("run execution: accepted interactive Question reply contains duplicate question id")
		}
		seen[binding.QuestionID] = struct{}{}
	}
	return q.record(ctx, interactiveQuestionReplyAcceptedEvent, map[string]any{
		"engine":   q.engine,
		"bindings": accepted,
	})
}

func (q *interactiveQuestioner) ListReplyAccepted(ctx context.Context) ([]engine.AcceptedInteractiveQuestionReply, error) {
	if q == nil || q.eventReader == nil || q.engine == "" {
		return nil, fmt.Errorf("run execution: interactive Question reply tracking is unavailable")
	}
	accepted := make(map[string]engine.AcceptedInteractiveQuestionReply)
	order := make([]string, 0)
	seenOrder := make(map[string]struct{})
	after := int64(0)
	for {
		events, err := q.eventReader.ListRunEvents(ctx, q.safe.Project.ID, q.safe.Run.ID, after, interactiveQuestionEventPageSize)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if event.Sequence == nil {
				return nil, fmt.Errorf("run execution: run event is missing sequence")
			}
			if *event.Sequence > after {
				after = *event.Sequence
			}
			switch event.Type {
			case interactiveQuestionReplyAcceptedEvent:
				var payload struct {
					Engine   string                                    `json:"engine"`
					Bindings []engine.AcceptedInteractiveQuestionReply `json:"bindings"`
				}
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					return nil, fmt.Errorf("run execution: decode accepted interactive Question reply: %w", err)
				}
				if payload.Engine != q.engine {
					continue
				}
				for _, binding := range payload.Bindings {
					if binding.QuestionID == "" || binding.CorrelationKey == "" {
						return nil, fmt.Errorf("run execution: accepted interactive Question reply event is malformed")
					}
					accepted[binding.QuestionID] = binding
					if _, exists := seenOrder[binding.QuestionID]; !exists {
						seenOrder[binding.QuestionID] = struct{}{}
						order = append(order, binding.QuestionID)
					}
				}
			case interactiveQuestionResolvedEvent:
				var payload struct {
					Engine     string `json:"engine"`
					QuestionID string `json:"questionId"`
				}
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					return nil, fmt.Errorf("run execution: decode resolved interactive Question reply: %w", err)
				}
				if payload.Engine == q.engine && payload.QuestionID != "" {
					delete(accepted, payload.QuestionID)
				}
			}
		}
		if len(events) < interactiveQuestionEventPageSize {
			break
		}
	}

	result := make([]engine.AcceptedInteractiveQuestionReply, 0, len(accepted))
	for _, questionID := range order {
		if binding, ok := accepted[questionID]; ok {
			result = append(result, binding)
		}
	}
	return result, nil
}

func (q *interactiveQuestioner) Resolve(ctx context.Context, questionID string) error {
	if q == nil || q.interactive == nil {
		return fmt.Errorf("run execution: interactive Question capability is unavailable")
	}
	result, err := q.interactive.ResolveInteractiveQuestion(ctx, q.safe.Project.ID, questionID)
	if err != nil {
		return err
	}
	if len(result.Events) != 0 {
		q.publishPersisted(ctx, result.Events)
		return nil
	}
	if err := q.record(ctx, interactiveQuestionResolvedEvent, map[string]any{
		"engine":     q.engine,
		"questionId": questionID,
	}); err != nil {
		return err
	}
	if result.Resumed {
		return q.record(ctx, "run.resumed", map[string]any{"questionId": questionID})
	}
	return nil
}

func (q *interactiveQuestioner) publishPersisted(ctx context.Context, events []store.Event) {
	if q == nil || q.events == nil {
		return
	}
	for _, event := range events {
		q.events.PublishPersisted(ctx, event)
	}
}

func (q *interactiveQuestioner) record(ctx context.Context, eventType string, payload any) error {
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return err
	}
	issueID, runID, agentID, workspaceID := q.safe.Issue.ID, q.safe.Run.ID, q.safe.Agent.ID, q.safe.Workspace.ID
	_, err = q.events.Record(ctx, store.Event{
		Type:        eventType,
		ProjectID:   q.safe.Project.ID,
		IssueID:     &issueID,
		RunID:       &runID,
		AgentID:     &agentID,
		WorkspaceID: &workspaceID,
		Actor:       store.EmptyObject,
		Payload:     encoded,
	})
	return err
}

var _ engine.InteractiveQuestioner = (*interactiveQuestioner)(nil)
var _ engine.InteractiveQuestionBatcher = (*interactiveQuestioner)(nil)
var _ engine.InteractiveQuestionReplyTracker = (*interactiveQuestioner)(nil)
