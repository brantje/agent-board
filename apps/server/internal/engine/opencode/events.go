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

type pendingMessage struct {
	key  string
	kind string
	text string
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
	case "session.idle":
		return s.flushPendingMessages(ctx)
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

	message, err := client.DecodeNativeErrorMessage(update.Error)
	if err != nil {
		return fmt.Errorf("opencode engine: decode native session error details: %w", err)
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

func decodeVisibleTextPart(data json.RawMessage) (completedTextPart, bool, bool, error) {
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
		return completedTextPart{}, false, false, err
	}
	if part.ID == "" || part.Ignored || strings.TrimSpace(part.Text) == "" {
		return completedTextPart{}, false, false, nil
	}
	complete := part.Time != nil && part.Time.End != nil
	return completedTextPart{ID: part.ID, Text: part.Text}, true, complete, nil
}

func decodeCompletedTextPart(data json.RawMessage) (completedTextPart, bool, error) {
	part, visible, complete, err := decodeVisibleTextPart(data)
	if err != nil || !visible || !complete {
		return completedTextPart{}, false, err
	}
	return part, true, nil
}

func (s *runState) persistAgentMessage(ctx context.Context, key, kind, text string) error {
	if _, duplicate := s.seenTextParts[key]; duplicate {
		delete(s.pendingMessages, key)
		return nil
	}
	if s.activity != nil {
		if err := s.activity.RecordActivity(ctx, engine.ActivityEvent{
			Type: "agent.message",
			Payload: map[string]any{
				"message": text,
				"kind":    kind,
				"source":  "opencode",
			},
		}); err != nil {
			return err
		}
	}
	s.seenTextParts[key] = struct{}{}
	delete(s.pendingMessages, key)
	if kind == "message" {
		s.lastVisibleMessage = text
	}
	return nil
}

func (s *runState) bufferOrPersistMessage(ctx context.Context, key, kind string, part completedTextPart, complete bool) error {
	if complete {
		return s.persistAgentMessage(ctx, key, kind, part.Text)
	}
	if _, exists := s.pendingMessages[key]; !exists {
		s.pendingMessageOrder = append(s.pendingMessageOrder, key)
	}
	s.pendingMessages[key] = pendingMessage{key: key, kind: kind, text: part.Text}
	return nil
}

func (s *runState) flushPendingMessages(ctx context.Context) error {
	for _, key := range s.pendingMessageOrder {
		message, ok := s.pendingMessages[key]
		if !ok {
			continue
		}
		if err := s.persistAgentMessage(ctx, message.key, message.kind, message.text); err != nil {
			return err
		}
	}
	s.pendingMessageOrder = nil
	return nil
}

func (s *runState) handleTextPart(ctx context.Context, data json.RawMessage) error {
	part, visible, complete, err := decodeVisibleTextPart(data)
	if err != nil {
		return fmt.Errorf("opencode engine: decode text part: %w", err)
	}
	if !visible {
		return nil
	}
	return s.bufferOrPersistMessage(ctx, part.ID, "message", part, complete)
}

func (s *runState) handleReasoningPart(ctx context.Context, data json.RawMessage) error {
	part, visible, complete, err := decodeVisibleTextPart(data)
	if err != nil {
		return fmt.Errorf("opencode engine: decode reasoning part: %w", err)
	}
	if !visible {
		return nil
	}
	return s.bufferOrPersistMessage(ctx, "reasoning/"+part.ID, "reasoning", part, complete)
}

func (s *runState) handleToolPart(ctx context.Context, data json.RawMessage) error {
	if err := s.flushPendingMessages(ctx); err != nil {
		return err
	}
	var part struct {
		ID     string `json:"id"`
		CallID string `json:"callID,omitempty"`
		Tool   string `json:"tool"`
		State  struct {
			Status string          `json:"status"`
			Input  json.RawMessage `json:"input,omitempty"`
			Output any             `json:"output,omitempty"`
			Title  string          `json:"title,omitempty"`
			Error  string          `json:"error,omitempty"`
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
		Input:      decodeToolInput(part.State.Input),
		Summary:    evidence.BoundActivityPreview(part.State.Title),
	}
	if eventType == "tool.completed" {
		payload.ResultPreview = toolResultPreview(part.State.Output)
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

func toolResultPreview(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return evidence.BoundActivityPreview(text)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return evidence.BoundActivityPreview(fmt.Sprint(value))
	}
	return evidence.BoundActivityPreview(string(encoded))
}

func decodeToolInput(raw json.RawMessage) map[string]any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(trimmed, &object); err == nil {
		return object
	}
	var encoded string
	if err := json.Unmarshal(trimmed, &encoded); err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(encoded), &object); err != nil {
		return nil
	}
	return object
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
