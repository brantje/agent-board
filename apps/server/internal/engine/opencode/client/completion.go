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
