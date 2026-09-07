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

const interactiveQuestionPollInterval = 200 * time.Millisecond

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
	if err := validateQuestionRequest(request); err != nil {
		return engine.Question{}, err
	}
	if !request.Blocking {
		return engine.Question{}, fmt.Errorf("run execution: interactive Questions must be blocking")
	}
	if correlationKey == "" {
		return engine.Question{}, fmt.Errorf("run execution: interactive Question correlation key is required")
	}

	options := make([]store.QuestionOption, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, store.QuestionOption{ID: option.ID, Label: option.Label})
	}
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return engine.Question{}, err
	}
	result, err := q.interactive.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
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
	})
	if err != nil {
		return engine.Question{}, err
	}

	if result.Created {
		if err := q.record(ctx, "question.created", map[string]any{
			"questionId": result.Question.ID,
			"prompt":     result.Question.Prompt,
			"kind":       result.Question.Kind,
			"options":    options,
			"blocking":   true,
		}); err != nil {
			return engine.Question{}, err
		}
	}
	if result.EnteredWaiting {
		if err := q.record(ctx, "run.waiting_for_input", map[string]any{"questionId": result.Question.ID}); err != nil {
			return engine.Question{}, err
		}
	}
	return engine.Question{ID: result.Question.ID, Blocking: true}, nil
}

func (q *interactiveQuestioner) WaitAnswer(ctx context.Context, questionID string) (engine.QuestionAnswer, error) {
	if q == nil || q.store == nil {
		return engine.QuestionAnswer{}, fmt.Errorf("run execution: interactive Question capability is unavailable")
	}
	if questionID == "" {
		return engine.QuestionAnswer{}, fmt.Errorf("run execution: interactive Question id is required")
	}

	ticker := time.NewTicker(interactiveQuestionPollInterval)
	defer ticker.Stop()
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

		select {
		case <-ctx.Done():
			return engine.QuestionAnswer{}, ctx.Err()
		case <-ticker.C:
		}
	}
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
