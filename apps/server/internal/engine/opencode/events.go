package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
)

type completedTextPart struct {
	ID   string
	Text string
}

func (s *runState) handleEvent(ctx context.Context, native *client.Client, event client.Event) error {
	switch event.Type {
	case "question.asked", "question.v2.asked":
		var request client.QuestionRequest
		if err := json.Unmarshal(event.Properties, &request); err != nil {
			return fmt.Errorf("opencode engine: decode native Question event: %w", err)
		}
		return s.handleQuestion(ctx, native, request)
	case "message.part.updated":
		return s.handlePartUpdated(ctx, event.Properties)
	case "session.error":
		return s.handleSessionError(event.Properties)
	default:
		return nil
	}
}

func (s *runState) handleSessionError(properties json.RawMessage) error {
	var update struct {
		SessionID string          `json:"sessionID"`
		Error     json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode native session error: %w", err)
	}
	if update.SessionID == "" || update.SessionID != s.sessionID {
		return nil
	}

	message := "native session failed"
	if len(bytes.TrimSpace(update.Error)) != 0 && !bytes.Equal(bytes.TrimSpace(update.Error), []byte("null")) {
		var nativeError struct {
			Name string `json:"name"`
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		}
		if err := json.Unmarshal(update.Error, &nativeError); err != nil {
			return fmt.Errorf("opencode engine: decode native session error details: %w", err)
		}
		if value := strings.TrimSpace(nativeError.Data.Message); value != "" {
			message = value
		} else if value := strings.TrimSpace(nativeError.Name); value != "" {
			message = value
		}
	}
	return fmt.Errorf("opencode engine: native session error: %s", message)
}

func (s *runState) handlePartUpdated(ctx context.Context, properties json.RawMessage) error {
	trimmed := bytes.TrimSpace(properties)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	var update struct {
		SessionID string          `json:"sessionID"`
		Part      json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode message part update: %w", err)
	}
	if update.SessionID != s.sessionID || len(update.Part) == 0 {
		return nil
	}
	var header struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(update.Part, &header); err != nil {
		return fmt.Errorf("opencode engine: decode message part header: %w", err)
	}
	if header.SessionID != "" && header.SessionID != s.sessionID {
		return nil
	}
	switch header.Type {
	case "text":
		return s.handleTextPart(ctx, update.Part)
	case "tool":
		return s.handleToolPart(ctx, update.Part)
	case "reasoning":
		return s.handleReasoningPart(ctx, update.Part)
	default:
		return nil
	}
}

func decodeCompletedTextPart(data json.RawMessage) (completedTextPart, bool, error) {
	var part struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
		Text      string `json:"text"`
		Ignored   bool   `json:"ignored,omitempty"`
		Time      *struct {
			End *float64 `json:"end,omitempty"`
		} `json:"time,omitempty"`
	}
	if err := json.Unmarshal(data, &part); err != nil {
		return completedTextPart{}, false, err
	}
	if part.ID == "" || part.Ignored || part.Time == nil || part.Time.End == nil || strings.TrimSpace(part.Text) == "" {
		return completedTextPart{}, false, nil
	}
	return completedTextPart{ID: part.ID, Text: part.Text}, true, nil
}

func (s *runState) handleTextPart(ctx context.Context, data json.RawMessage) error {
	part, complete, err := decodeCompletedTextPart(data)
	if err != nil {
		return fmt.Errorf("opencode engine: decode text part: %w", err)
	}
	if !complete {
		return nil
	}
	if _, duplicate := s.seenTextParts[part.ID]; duplicate {
		return nil
	}
	if s.activity != nil {
		if err := s.activity.RecordActivity(ctx, engine.ActivityEvent{
			Type: "agent.message",
			Payload: map[string]any{
				"message": part.Text,
				"kind":    "message",
				"source":  "opencode",
			},
		}); err != nil {
			return err
		}
	}
	s.seenTextParts[part.ID] = struct{}{}
	s.lastVisibleMessage = part.Text
	return nil
}

func (s *runState) handleReasoningPart(ctx context.Context, data json.RawMessage) error {
	part, complete, err := decodeCompletedTextPart(data)
	if err != nil {
		return fmt.Errorf("opencode engine: decode reasoning part: %w", err)
	}
	if !complete {
		return nil
	}
	key := "reasoning/" + part.ID
	if _, duplicate := s.seenTextParts[key]; duplicate {
		return nil
	}
	if s.activity != nil {
		if err := s.activity.RecordActivity(ctx, engine.ActivityEvent{
			Type: "agent.message",
			Payload: map[string]any{
				"message": part.Text,
				"kind":    "reasoning",
				"source":  "opencode",
			},
		}); err != nil {
			return err
		}
	}
	s.seenTextParts[key] = struct{}{}
	return nil
}

func (s *runState) handleToolPart(ctx context.Context, data json.RawMessage) error {
	var part struct {
		ID     string `json:"id"`
		CallID string `json:"callID,omitempty"`
		Tool   string `json:"tool"`
		State  struct {
			Status string         `json:"status"`
			Input  map[string]any `json:"input,omitempty"`
			Output string         `json:"output,omitempty"`
			Title  string         `json:"title,omitempty"`
			Error  string         `json:"error,omitempty"`
		} `json:"state"`
	}
	if err := json.Unmarshal(data, &part); err != nil {
		return fmt.Errorf("opencode engine: decode tool part: %w", err)
	}
	if part.ID == "" || strings.TrimSpace(part.Tool) == "" {
		return nil
	}
	var eventType string
	switch part.State.Status {
	case "running":
		eventType = "tool.started"
	case "completed":
		eventType = "tool.completed"
	case "error":
		eventType = "tool.failed"
	default:
		return nil
	}
	key := part.ID + "/" + part.State.Status
	if _, duplicate := s.seenToolStates[key]; duplicate {
		return nil
	}
	toolCallID := strings.TrimSpace(part.CallID)
	if toolCallID == "" {
		toolCallID = part.ID
	}
	payload := evidence.ToolPayload{
		Kind:       "tool",
		Name:       part.Tool,
		ToolCallID: toolCallID,
		Input:      part.State.Input,
		Summary:    evidence.BoundActivityPreview(part.State.Title),
	}
	if eventType == "tool.completed" {
		payload.ResultPreview = evidence.BoundActivityPreview(part.State.Output)
	}
	if eventType == "tool.failed" {
		payload.Reason = evidence.BoundActivityPreview(part.State.Error)
	}
	if s.activity != nil {
		if err := s.activity.RecordActivity(ctx, engine.ActivityEvent{Type: eventType, Payload: toolPayloadMap(payload)}); err != nil {
			return err
		}
	}
	s.seenToolStates[key] = struct{}{}
	return nil
}

func toolPayloadMap(payload evidence.ToolPayload) map[string]any {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return map[string]any{"name": payload.Name, "toolCallId": payload.ToolCallID}
	}
	var mapped map[string]any
	if err := json.Unmarshal(encoded, &mapped); err != nil {
		return map[string]any{"name": payload.Name, "toolCallId": payload.ToolCallID}
	}
	mapped["source"] = "opencode"
	return mapped
}
