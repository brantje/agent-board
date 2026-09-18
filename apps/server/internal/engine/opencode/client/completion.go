package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type SessionMessage struct {
	Info  json.RawMessage   `json:"info"`
	Parts []json.RawMessage `json:"parts"`
}

func (c *Client) ListMessages(ctx context.Context, sessionID string) ([]SessionMessage, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("opencode: session id is required")
	}
	path := "/session/" + url.PathEscape(sessionID) + "/message"
	raw, err := c.getRawJSON(ctx, path)
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var messages []SessionMessage
	if err := json.Unmarshal(raw, &messages); err == nil {
		return messages, nil
	}
	var wrapped struct {
		Data []SessionMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("opencode: decode session messages: %w", err)
	}
	return wrapped.Data, nil
}

// PromptAdmitted proves that the exact expected user turn is present in the
// authoritative native session history. An empty history means the recovered
// session was created before its first Prompt. Non-empty history without the
// expected turn is ambiguous and must not be reused for a different task.
func (c *Client) PromptAdmitted(ctx context.Context, sessionID, expected string) (bool, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(expected) == "" {
		return false, fmt.Errorf("opencode: session id and expected prompt are required")
	}
	messages, err := c.ListMessages(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if len(messages) == 0 {
		return false, nil
	}
	for _, message := range messages {
		var info struct {
			Role string `json:"role"`
		}
		if len(bytes.TrimSpace(message.Info)) == 0 {
			return false, fmt.Errorf("opencode: native session history has message without info")
		}
		if err := json.Unmarshal(message.Info, &info); err != nil {
			return false, fmt.Errorf("opencode: decode native session message info: %w", err)
		}
		if info.Role != "user" {
			continue
		}
		for _, raw := range message.Parts {
			var part struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &part); err != nil {
				return false, fmt.Errorf("opencode: decode native user message part: %w", err)
			}
			if part.Type == "text" && part.Text == expected {
				return true, nil
			}
		}
	}
	return false, fmt.Errorf("opencode: recovered native session has history but the expected prompt cannot be proven")
}

// LatestAssistantError checks the newest native session message for a durable
// assistant failure. OpenCode can persist a provider/API error on the assistant
// message even when the corresponding session.error SSE event is missed.
func (c *Client) LatestAssistantError(ctx context.Context, sessionID string) (string, bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", false, fmt.Errorf("opencode: session id is required")
	}

	var messages []struct {
		Info struct {
			Role  string          `json:"role"`
			Error json.RawMessage `json:"error"`
		} `json:"info"`
	}
	path := "/session/" + url.PathEscape(sessionID) + "/message?limit=1"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &messages); err != nil {
		// The pinned Runtime exposes the legacy message-history route above. Keep
		// deterministic compatibility fixtures that predate that route usable;
		// they still exercise the session.error SSE failure path.
		if isNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if len(messages) == 0 {
		return "", false, nil
	}
	latest := messages[len(messages)-1].Info
	if latest.Role != "assistant" || !hasJSONValue(latest.Error) {
		return "", false, nil
	}
	message, err := DecodeNativeErrorMessage(latest.Error)
	if err != nil {
		return "", false, fmt.Errorf("opencode: decode latest assistant error: %w", err)
	}
	return message, true, nil
}

// DecodeNativeErrorMessage extracts the user-safe message from OpenCode's
// NamedError wire shape used by both session.error events and assistant history.
func DecodeNativeErrorMessage(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "native session failed", nil
	}
	var nativeError struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &nativeError); err != nil {
		return "", err
	}
	if message := strings.TrimSpace(nativeError.Data.Message); message != "" {
		return message, nil
	}
	if name := strings.TrimSpace(nativeError.Name); name != "" {
		return name, nil
	}
	return "native session failed", nil
}
