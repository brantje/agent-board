package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/modelusage"
)

type modelStepState struct {
	messageID     string
	startedAt     *time.Time
	firstOutputAt *time.Time
	completedAt   *time.Time
}

type usagePartHeader struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
}

type nativePartTime struct {
	Start *float64 `json:"start,omitempty"`
	End   *float64 `json:"end,omitempty"`
}

func (s *runState) seedModelUsage(providerID, modelID string, limit *int64) {
	s.providerID = strings.TrimSpace(providerID)
	s.modelID = strings.TrimSpace(modelID)
	s.contextLimitTokens = cloneInt64(limit)
}

func (s *runState) backfillUsageFromHistory(ctx context.Context, native *client.Client) error {
	if s == nil || s.usage == nil || native == nil {
		return nil
	}
	messages, err := native.ListMessages(ctx, s.sessionID)
	if err != nil || len(messages) == 0 {
		return nil
	}
	for _, message := range messages {
		if err := s.replayUsageMessage(ctx, native, message); err != nil {
			continue
		}
	}
	return nil
}

func (s *runState) replayUsageMessage(ctx context.Context, native *client.Client, message client.SessionMessage) error {
	if len(bytes.TrimSpace(message.Info)) == 0 {
		return nil
	}
	infoEnvelope, err := json.Marshal(map[string]json.RawMessage{"info": message.Info})
	if err != nil {
		return fmt.Errorf("opencode engine: encode usage history message: %w", err)
	}
	if err := s.handleUsageMessageUpdated(ctx, native, infoEnvelope); err != nil {
		return err
	}
	sessionID := s.sessionID
	var header struct {
		SessionID string `json:"sessionID"`
		Role      string `json:"role"`
	}
	if err := json.Unmarshal(message.Info, &header); err != nil {
		return fmt.Errorf("opencode engine: decode usage history message header: %w", err)
	}
	if header.SessionID != "" {
		sessionID = header.SessionID
	}
	if header.Role != "" && header.Role != "assistant" {
		return nil
	}
	for _, part := range message.Parts {
		envelope, err := json.Marshal(struct {
			SessionID string          `json:"sessionID"`
			Part      json.RawMessage `json:"part"`
		}{SessionID: sessionID, Part: part})
		if err != nil {
			return fmt.Errorf("opencode engine: encode usage history part: %w", err)
		}
		if err := s.handleUsagePartUpdated(ctx, envelope); err != nil {
			return err
		}
	}
	return nil
}

func (s *runState) handleUsageMessageUpdated(ctx context.Context, native *client.Client, properties json.RawMessage) error {
	if s.usage == nil {
		return nil
	}
	var update struct {
		SessionID string `json:"sessionID"`
		Info      struct {
			ID         string `json:"id"`
			SessionID  string `json:"sessionID"`
			Role       string `json:"role"`
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
			Time       *struct {
				Created *float64 `json:"created,omitempty"`
			} `json:"time,omitempty"`
		} `json:"info"`
	}
	if err := json.Unmarshal(properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode usage message update: %w", err)
	}
	sessionID := update.SessionID
	if sessionID == "" {
		sessionID = update.Info.SessionID
	}
	if sessionID != s.sessionID || update.Info.Role != "assistant" {
		return nil
	}
	if update.Info.Time != nil {
		s.rememberAssistantStart(update.Info.ID, nativeTimeMillis(update.Info.Time.Created))
	}
	if err := s.enrichRecordedUsage(ctx, update.Info.ID); err != nil {
		return err
	}
	providerID := strings.TrimSpace(update.Info.ProviderID)
	modelID := strings.TrimSpace(update.Info.ModelID)
	if providerID == "" || modelID == "" {
		return nil
	}
	if providerID == s.providerID && modelID == s.modelID {
		return nil
	}
	s.providerID = providerID
	s.modelID = modelID
	s.contextLimitTokens = nil
	if native == nil {
		return nil
	}
	limit, err := native.ModelContextLimit(ctx, providerID, modelID)
	if err != nil {
		return nil
	}
	s.contextLimitTokens = cloneInt64(limit)
	return nil
}

func (s *runState) handleUsagePartUpdated(ctx context.Context, properties json.RawMessage) error {
	if s.usage == nil {
		return nil
	}
	trimmed := bytes.TrimSpace(properties)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var update struct {
		SessionID string          `json:"sessionID"`
		Part      json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode usage part update: %w", err)
	}
	if len(update.Part) == 0 {
		return nil
	}
	var header usagePartHeader
	if err := json.Unmarshal(update.Part, &header); err != nil {
		return fmt.Errorf("opencode engine: decode usage part header: %w", err)
	}
	sessionID := strings.TrimSpace(update.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(header.SessionID)
	}
	if sessionID != s.sessionID {
		return nil
	}

	switch header.Type {
	case "step-start":
		s.beginModelStep(header.MessageID)
		return nil
	case "text", "reasoning", "tool":
		return s.handleUsageGeneratedPart(header, update.Part)
	case "step-finish":
		return s.handleUsageStepFinish(ctx, header, update.Part)
	default:
		return nil
	}
}

func (s *runState) beginModelStep(messageID string) {
	if s.activeModelStep != nil && sameModelMessage(messageID, s.activeModelStep.messageID) {
		if s.activeModelStep.startedAt == nil {
			s.applyAssistantStart(s.activeModelStep)
		}
		return
	}
	s.activeModelStep = &modelStepState{messageID: messageID}
	s.applyAssistantStart(s.activeModelStep)
}

func (s *runState) ensureModelStep(messageID string) *modelStepState {
	if s.activeModelStep == nil || !sameModelMessage(messageID, s.activeModelStep.messageID) {
		s.beginModelStep(messageID)
	}
	return s.activeModelStep
}

func (s *runState) handleUsageGeneratedPart(header usagePartHeader, data json.RawMessage) error {
	step := s.ensureModelStep(header.MessageID)
	if step == nil {
		return nil
	}
	start, end, err := decodeUsagePartTiming(header.Type, data)
	if err != nil {
		return fmt.Errorf("opencode engine: decode usage part time: %w", err)
	}
	if start != nil {
		switch header.Type {
		case "reasoning", "text":
			if step.firstOutputAt == nil || start.Before(*step.firstOutputAt) {
				step.firstOutputAt = cloneTime(start)
			}
		case "tool":
			if step.firstOutputAt == nil {
				started, err := generatedOutputStarted(header.Type, data)
				if err != nil {
					return err
				}
				if started {
					step.firstOutputAt = cloneTime(start)
				}
			}
		}
	}
	if header.Type != "tool" && end != nil {
		if step.completedAt == nil || end.After(*step.completedAt) {
			step.completedAt = cloneTime(end)
		}
	}
	return nil
}

func (s *runState) handleUsageStepFinish(ctx context.Context, header usagePartHeader, data json.RawMessage) error {
	if strings.TrimSpace(header.ID) == "" || s.providerID == "" || s.modelID == "" {
		return nil
	}
	sampleID := "opencode:" + s.sessionID + ":" + header.ID
	if _, duplicate := s.seenUsageSamples[sampleID]; duplicate {
		return nil
	}
	var part struct {
		Tokens struct {
			Input     int64 `json:"input"`
			Output    int64 `json:"output"`
			Reasoning int64 `json:"reasoning"`
			Cache     struct {
				Read  int64 `json:"read"`
				Write int64 `json:"write"`
			} `json:"cache"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &part); err != nil {
		return fmt.Errorf("opencode engine: decode step-finish usage: %w", err)
	}
	sample := modelusage.Sample{
		SampleID:           sampleID,
		ProviderID:         s.providerID,
		ModelID:            s.modelID,
		InputTokens:        part.Tokens.Input,
		OutputTokens:       part.Tokens.Output,
		ReasoningTokens:    part.Tokens.Reasoning,
		CacheReadTokens:    part.Tokens.Cache.Read,
		CacheWriteTokens:   part.Tokens.Cache.Write,
		ContextTokens:      part.Tokens.Input + part.Tokens.Output + part.Tokens.Reasoning + part.Tokens.Cache.Read + part.Tokens.Cache.Write,
		ContextLimitTokens: cloneInt64(s.contextLimitTokens),
	}
	if s.activeModelStep != nil && sameModelMessage(header.MessageID, s.activeModelStep.messageID) {
		sample.StartedAt = cloneTime(s.activeModelStep.startedAt)
		sample.FirstOutputAt = cloneTime(s.activeModelStep.firstOutputAt)
		sample.CompletedAt = cloneTime(s.activeModelStep.completedAt)
	}
	if sample.StartedAt == nil {
		if messageID := strings.TrimSpace(header.MessageID); messageID != "" {
			if _, laterStep := s.usageSampleMessages[messageID]; !laterStep {
				sample.StartedAt = cloneTime(s.assistantStartedAt[messageID])
			}
		}
	}
	if sample.CompletedAt == nil && sample.FirstOutputAt != nil {
		now := s.usageClock()
		if now.After(*sample.FirstOutputAt) {
			sample.CompletedAt = cloneTime(&now)
		}
	}
	if err := s.usage.RecordModelUsage(ctx, sample); err != nil {
		return err
	}
	s.seenUsageSamples[sampleID] = struct{}{}
	s.recordedUsageSamples[sampleID] = sample
	if messageID := strings.TrimSpace(header.MessageID); messageID != "" {
		if _, exists := s.messageFirstSampleID[messageID]; !exists {
			s.messageFirstSampleID[messageID] = sampleID
		}
		s.usageSampleMessages[messageID] = struct{}{}
	}
	if s.activeModelStep != nil && sameModelMessage(header.MessageID, s.activeModelStep.messageID) {
		s.activeModelStep = nil
	}
	return nil
}

func (s *runState) enrichRecordedUsage(ctx context.Context, messageID string) error {
	if s == nil || s.usage == nil {
		return nil
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	sampleID := s.messageFirstSampleID[messageID]
	if sampleID == "" {
		return nil
	}
	sample, ok := s.recordedUsageSamples[sampleID]
	if !ok || sample.StartedAt != nil {
		return nil
	}
	startedAt := s.assistantStartedAt[messageID]
	if startedAt == nil {
		return nil
	}
	sample.StartedAt = cloneTime(startedAt)
	if err := s.usage.RecordModelUsage(ctx, sample); err != nil {
		return err
	}
	s.recordedUsageSamples[sampleID] = sample
	return nil
}

func sameModelMessage(left, right string) bool {
	return left == "" || right == "" || left == right
}

func generatedOutputStarted(partType string, data json.RawMessage) (bool, error) {
	switch partType {
	case "text", "reasoning":
		var part struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &part); err != nil {
			return false, fmt.Errorf("opencode engine: decode %s timing part: %w", partType, err)
		}
		return strings.TrimSpace(part.Text) != "", nil
	case "tool":
		var part struct {
			Tool string `json:"tool"`
		}
		if err := json.Unmarshal(data, &part); err != nil {
			return false, fmt.Errorf("opencode engine: decode tool timing part: %w", err)
		}
		return strings.TrimSpace(part.Tool) != "", nil
	default:
		return false, nil
	}
}

func decodeNativePartTime(data json.RawMessage) (*nativePartTime, error) {
	var part struct {
		Time *nativePartTime `json:"time,omitempty"`
	}
	if err := json.Unmarshal(data, &part); err != nil {
		return nil, err
	}
	return part.Time, nil
}

func decodeUsagePartTiming(partType string, data json.RawMessage) (start, end *time.Time, err error) {
	var partTime *nativePartTime
	switch partType {
	case "text", "reasoning":
		partTime, err = decodeNativePartTime(data)
	case "tool":
		var part struct {
			State struct {
				Time *nativePartTime `json:"time,omitempty"`
			} `json:"state"`
		}
		if err = json.Unmarshal(data, &part); err != nil {
			return nil, nil, err
		}
		partTime = part.State.Time
	default:
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if partTime == nil {
		return nil, nil, nil
	}
	return nativeTimeMillis(partTime.Start), nativeTimeMillis(partTime.End), nil
}

func (s *runState) rememberAssistantStart(messageID string, startedAt *time.Time) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || startedAt == nil {
		return
	}
	if s.assistantStartedAt[messageID] == nil {
		s.assistantStartedAt[messageID] = cloneTime(startedAt)
	}
	s.applyAssistantStart(s.activeModelStep)
}

func (s *runState) applyAssistantStart(step *modelStepState) {
	if step == nil || step.startedAt != nil {
		return
	}
	messageID := strings.TrimSpace(step.messageID)
	if messageID == "" {
		return
	}
	if _, recorded := s.usageSampleMessages[messageID]; recorded {
		return
	}
	if startedAt := s.assistantStartedAt[messageID]; startedAt != nil {
		step.startedAt = cloneTime(startedAt)
	}
}

func nativeTimeMillis(value *float64) *time.Time {
	if value == nil || *value <= 0 {
		return nil
	}
	return nativeEventTime(*value)
}

func (s *runState) usageClock() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func nativeEventTime(milliseconds float64) *time.Time {
	if milliseconds <= 0 {
		return nil
	}
	value := time.UnixMilli(int64(milliseconds)).UTC()
	return &value
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
