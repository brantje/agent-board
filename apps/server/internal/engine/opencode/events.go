package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func (s *runState) handleEvent(ctx context.Context, native *client.Client, event client.Event) error {
	switch event.Type {
	case "question.v2.asked":
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
	// The event stream is instance-scoped and may contain errors for another
	// session. Only the native session owned by this Run is terminal here.
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
	// message.part.updated is best-effort visible telemetry. OpenCode can emit
	// an envelope before the optional properties payload is populated; there is
	// no control state to recover from that empty update, so ignore it and wait
	// for a later complete part update instead of failing the Run.
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
		// Native reasoning parts may contain private chain-of-thought. Agent
		// Board intentionally does not persist or transform them.
		return nil
	default:
		return nil
	}
}

func (s *runState) handleTextPart(ctx context.Context, data json.RawMessage) error {
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
		return fmt.Errorf("opencode engine: decode text part: %w", err)
	}
	// message.part.updated can publish the same whole text part repeatedly as
	// it streams. Persist only the final explicit visible text once.
	if part.ID == "" || part.Ignored || part.Time == nil || part.Time.End == nil || strings.TrimSpace(part.Text) == "" {
		return nil
	}
	if _, duplicate := s.seenTextParts[part.ID]; duplicate {
		return nil
	}
	s.seenTextParts[part.ID] = struct{}{}
	s.lastVisibleMessage = part.Text
	if s.activity == nil {
		return nil
	}
	return s.activity.RecordActivity(ctx, engine.ActivityEvent{
		Type: "agent.message",
		Payload: map[string]any{
			"message": part.Text,
			"kind":    "message",
			"source":  "opencode",
		},
	})
}

func (s *runState) handleToolPart(ctx context.Context, data json.RawMessage) error {
	var part struct {
		ID   string `json:"id"`
		Tool string `json:"tool"`
		State struct {
			Status string `json:"status"`
			Title  string `json:"title,omitempty"`
			Error  string `json:"error,omitempty"`
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
	s.seenToolStates[key] = struct{}{}
	if s.activity == nil {
		return nil
	}
	payload := map[string]any{
		"name":   part.Tool,
		"source": "opencode",
	}
	if title := strings.TrimSpace(part.State.Title); title != "" {
		payload["summary"] = title
	}
	if eventType == "tool.failed" && strings.TrimSpace(part.State.Error) != "" {
		payload["reason"] = part.State.Error
	}
	return s.activity.RecordActivity(ctx, engine.ActivityEvent{Type: eventType, Payload: payload})
}
