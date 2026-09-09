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
}

type usagePartHeader struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
}

func (s *runState) handleUsageMessageUpdated(ctx context.Context, native *client.Client, properties json.RawMessage) error {
	if s.usage == nil {
		return nil
	}
	var update struct {
		SessionID string `json:"sessionID"`
		Info      struct {
			SessionID  string `json:"sessionID"`
			Role       string `json:"role"`
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
		} `json:"info"`
	}
	if err := json.Unmarshal(properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode usage message update: %w", err)
	}
	if update.SessionID != s.sessionID || (update.Info.SessionID != "" && update.Info.SessionID != s.sessionID) || update.Info.Role != "assistant" {
		return nil
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
		Time      float64         `json:"time"`
	}
	if err := json.Unmarshal(properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode usage part update: %w", err)
	}
	if update.SessionID != s.sessionID || len(update.Part) == 0 {
		return nil
	}
	var header usagePartHeader
	if err := json.Unmarshal(update.Part, &header); err != nil {
		return fmt.Errorf("opencode engine: decode usage part header: %w", err)
	}
	if header.SessionID != "" && header.SessionID != s.sessionID {
		return nil
	}

	switch header.Type {
	case "step-start":
		s.activeModelStep = &modelStepState{messageID: header.MessageID, startedAt: nativeEventTime(update.Time)}
		return nil
	case "text", "reasoning", "tool":
		if s.activeModelStep == nil || s.activeModelStep.firstOutputAt != nil {
			return nil
		}
		if header.MessageID != "" && s.activeModelStep.messageID != "" && header.MessageID != s.activeModelStep.messageID {
			return nil
		}
		started, err := generatedOutputStarted(header.Type, update.Part)
		if err != nil {
			return err
		}
		if started {
			s.activeModelStep.firstOutputAt = nativeEventTime(update.Time)
		}
		return nil
	case "step-finish":
		return s.handleUsageStepFinish(ctx, header, update.Part, update.Time)
	default:
		return nil
	}
}

func (s *runState) handleUsageStepFinish(ctx context.Context, header usagePartHeader, data json.RawMessage, eventTime float64) error {
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
		CompletedAt:        nativeEventTime(eventTime),
	}
	if s.activeModelStep != nil && sameModelMessage(header.MessageID, s.activeModelStep.messageID) {
		sample.StartedAt = cloneTime(s.activeModelStep.startedAt)
		sample.FirstOutputAt = cloneTime(s.activeModelStep.firstOutputAt)
	}
	if err := s.usage.RecordModelUsage(ctx, sample); err != nil {
		return err
	}
	s.seenUsageSamples[sampleID] = struct{}{}
	if s.activeModelStep != nil && sameModelMessage(header.MessageID, s.activeModelStep.messageID) {
		s.activeModelStep = nil
	}
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
