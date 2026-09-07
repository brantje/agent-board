package opencode

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

type nativeQuestionBinding struct {
	questionID string
	kind       string
	labels     map[string]string
}

type nativeQuestionState struct {
	bindings []nativeQuestionBinding
	answered bool
	replied  bool
}

type runState struct {
	sessionID          string
	questions          engine.InteractiveQuestioner
	activity           engine.ActivitySink
	nativeQuestions    map[string]*nativeQuestionState
	seenTextParts      map[string]struct{}
	seenToolStates     map[string]struct{}
	lastVisibleMessage string
}

func newRunState(sessionID string, questions engine.InteractiveQuestioner, activity engine.ActivitySink) *runState {
	return &runState{
		sessionID:       sessionID,
		questions:       questions,
		activity:        activity,
		nativeQuestions: make(map[string]*nativeQuestionState),
		seenTextParts:   make(map[string]struct{}),
		seenToolStates:  make(map[string]struct{}),
	}
}

func (s *runState) handlePending(ctx context.Context, native *client.Client, requests []client.QuestionRequest) error {
	for _, request := range requests {
		if request.SessionID != s.sessionID {
			continue
		}
		if err := s.handleQuestion(ctx, native, request); err != nil {
			return err
		}
	}
	return nil
}

func (s *runState) handleQuestion(ctx context.Context, native *client.Client, request client.QuestionRequest) error {
	if request.SessionID != s.sessionID {
		return nil
	}
	if strings.TrimSpace(request.ID) == "" || len(request.Questions) == 0 {
		return fmt.Errorf("opencode engine: invalid native Question request")
	}
	state := s.nativeQuestions[request.ID]
	if state != nil && state.replied {
		return nil
	}
	if state == nil {
		state = &nativeQuestionState{}
		s.nativeQuestions[request.ID] = state
	}

	if len(state.bindings) == 0 {
		openRequests := make([]engine.CorrelatedQuestionRequest, len(request.Questions))
		bindings := make([]nativeQuestionBinding, len(request.Questions))
		for index, nativeQuestion := range request.Questions {
			mapped, labels, err := mapNativeQuestion(nativeQuestion)
			if err != nil {
				return fmt.Errorf("opencode engine: map native Question %s[%d]: %w", request.ID, index, err)
			}
			openRequests[index] = engine.CorrelatedQuestionRequest{
				CorrelationKey: nativeCorrelationKey(s.sessionID, request.ID, index),
				Question:       mapped,
			}
			bindings[index] = nativeQuestionBinding{kind: mapped.Kind, labels: labels}
		}

		opened, err := s.openQuestionBatch(ctx, openRequests)
		if err != nil {
			return fmt.Errorf("opencode engine: open native Question %s: %w", request.ID, err)
		}
		if len(opened) != len(bindings) {
			return fmt.Errorf("opencode engine: native Question %s opened %d bindings for %d Questions", request.ID, len(opened), len(bindings))
		}
		for index, question := range opened {
			bindings[index].questionID = question.ID
		}
		state.bindings = bindings
	}
	if len(state.bindings) != len(request.Questions) {
		return fmt.Errorf("opencode engine: native Question %s changed shape during reconciliation", request.ID)
	}

	answers := make([][]string, len(state.bindings))
	for index, binding := range state.bindings {
		answer, err := s.questions.WaitAnswer(ctx, binding.questionID)
		if err != nil {
			return fmt.Errorf("opencode engine: wait for Question %s[%d]: %w", request.ID, index, err)
		}
		mapped, err := mapCanonicalAnswer(binding, answer)
		if err != nil {
			return fmt.Errorf("opencode engine: map answer for Question %s[%d]: %w", request.ID, index, err)
		}
		answers[index] = mapped
	}
	state.answered = true

	if err := replyNativeQuestion(ctx, native, s.sessionID, request.ID, answers); err != nil {
		return err
	}
	for index, binding := range state.bindings {
		if err := s.questions.Resolve(ctx, binding.questionID); err != nil {
			return fmt.Errorf("opencode engine: resolve Question %s[%d]: %w", request.ID, index, err)
		}
	}
	state.replied = true
	return nil
}

func (s *runState) openQuestionBatch(ctx context.Context, requests []engine.CorrelatedQuestionRequest) ([]engine.Question, error) {
	if batcher, ok := s.questions.(engine.InteractiveQuestionBatcher); ok {
		return batcher.OpenBatch(ctx, requests)
	}
	if len(requests) != 1 {
		return nil, fmt.Errorf("interactive Question batch capability is required for %d Questions", len(requests))
	}
	question, err := s.questions.Open(ctx, requests[0].CorrelationKey, requests[0].Question)
	if err != nil {
		return nil, err
	}
	return []engine.Question{question}, nil
}

func mapNativeQuestion(native client.QuestionInfo) (engine.QuestionRequest, map[string]string, error) {
	prompt := strings.TrimSpace(native.Question)
	if prompt == "" {
		return engine.QuestionRequest{}, nil, fmt.Errorf("native Question prompt is empty")
	}
	request := engine.QuestionRequest{Prompt: prompt, Blocking: true}
	labels := make(map[string]string, len(native.Options))
	if len(native.Options) == 0 {
		request.Kind = "TEXT"
		return request, labels, nil
	}
	request.Kind = "SINGLE_CHOICE"
	if native.Multiple != nil && *native.Multiple {
		request.Kind = "MULTI_CHOICE"
	}
	request.Options = make([]engine.QuestionOption, 0, len(native.Options))
	for index, option := range native.Options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			return engine.QuestionRequest{}, nil, fmt.Errorf("native Question option %d has no label", index)
		}
		id := fmt.Sprintf("option-%d", index)
		request.Options = append(request.Options, engine.QuestionOption{ID: id, Label: label})
		labels[id] = label
	}
	return request, labels, nil
}

func mapCanonicalAnswer(binding nativeQuestionBinding, answer engine.QuestionAnswer) ([]string, error) {
	if answer.Kind != binding.kind {
		return nil, fmt.Errorf("answer kind %q does not match Question kind %q", answer.Kind, binding.kind)
	}
	switch binding.kind {
	case "TEXT":
		if answer.Text == nil || strings.TrimSpace(*answer.Text) == "" {
			return nil, fmt.Errorf("text answer is empty")
		}
		return []string{*answer.Text}, nil
	case "SINGLE_CHOICE", "MULTI_CHOICE":
		if len(answer.OptionIDs) == 0 {
			return nil, fmt.Errorf("choice answer has no options")
		}
		labels := make([]string, 0, len(answer.OptionIDs))
		for _, optionID := range answer.OptionIDs {
			label, ok := binding.labels[optionID]
			if !ok {
				return nil, fmt.Errorf("unknown option id %q", optionID)
			}
			labels = append(labels, label)
		}
		return labels, nil
	default:
		return nil, fmt.Errorf("unsupported Question kind %q", binding.kind)
	}
}

func nativeCorrelationKey(sessionID, requestID string, index int) string {
	return fmt.Sprintf("%s/%s/%d", sessionID, requestID, index)
}

func replyNativeQuestion(ctx context.Context, native *client.Client, sessionID, requestID string, answers [][]string) error {
	if err := native.ReplyQuestion(ctx, sessionID, requestID, answers); err == nil {
		return nil
	} else {
		firstErr := err
		pending, listErr := native.ListQuestions(ctx, sessionID)
		if listErr != nil {
			return errors.Join(
				fmt.Errorf("opencode engine: reply native Question %s: %w", requestID, firstErr),
				fmt.Errorf("reconcile native Question reply: %w", listErr),
			)
		}
		stillPending := false
		for _, request := range pending {
			if request.ID == requestID {
				stillPending = true
				break
			}
		}
		if !stillPending {
			// The reply may have reached OpenCode before the transport failed.
			// Absence from the authoritative pending list means no second inference
			// call is needed; resolving the durable bindings is safe and idempotent.
			return nil
		}
		if retryErr := native.ReplyQuestion(ctx, sessionID, requestID, answers); retryErr != nil {
			return errors.Join(fmt.Errorf("opencode engine: reply native Question %s: %w", requestID, firstErr), retryErr)
		}
		return nil
	}
}

func (s *runState) activeNativeRequests() map[string]struct{} {
	active := make(map[string]struct{})
	for requestID, state := range s.nativeQuestions {
		if state != nil && !state.replied {
			active[requestID] = struct{}{}
		}
	}
	return active
}
