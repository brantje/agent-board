package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type PermissionRequest struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionID"`
	Permission string          `json:"permission"`
	Patterns   []string        `json:"patterns"`
	Metadata   json.RawMessage `json:"metadata"`
	Always     []string        `json:"always"`
	Tool       *struct {
		MessageID string `json:"messageID"`
		CallID    string `json:"callID"`
	} `json:"tool,omitempty"`
}

func (c *Client) ListPermissions(ctx context.Context, sessionID string) ([]PermissionRequest, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("opencode: session id is required")
	}
	raw, err := c.getRawJSON(ctx, "/permission")
	if isNotFound(err) {
		raw, err = c.getRawJSON(ctx, "/api/permission/request")
	}
	if err != nil {
		return nil, err
	}
	var values []PermissionRequest
	if err := json.Unmarshal(raw, &values); err != nil {
		var wrapped struct {
			Data []PermissionRequest `json:"data"`
		}
		if wrappedErr := json.Unmarshal(raw, &wrapped); wrappedErr != nil {
			return nil, fmt.Errorf("opencode: decode permission list: %w", err)
		}
		values = wrapped.Data
	}
	filtered := make([]PermissionRequest, 0, len(values))
	for _, value := range values {
		if value.SessionID == sessionID {
			filtered = append(filtered, value)
		}
	}
	return filtered, nil
}

func (c *Client) ReplyPermission(ctx context.Context, sessionID, requestID, response string) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("opencode: session id and permission request id are required")
	}
	switch response {
	case "once", "always", "reject":
	default:
		return fmt.Errorf("opencode: unsupported permission response %q", response)
	}
	legacyPayload := struct {
		Reply string `json:"reply"`
	}{Reply: response}
	legacyPath := "/permission/" + url.PathEscape(requestID) + "/reply"
	if err := c.doJSON(ctx, http.MethodPost, legacyPath, legacyPayload, nil); err == nil {
		return nil
	} else if !isNotFound(err) {
		return err
	}
	payload := struct {
		Response string `json:"response"`
	}{Response: response}
	path := "/session/" + url.PathEscape(sessionID) + "/permissions/" + url.PathEscape(requestID)
	return c.doJSON(ctx, http.MethodPost, path, payload, nil)
}

func (c *Client) ToolIDs(ctx context.Context) ([]string, error) {
	raw, err := c.getRawJSON(ctx, "/experimental/tool/ids")
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err == nil {
		return ids, nil
	}
	var wrapped struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("opencode: decode tool ids: %w", err)
	}
	return wrapped.Data, nil
}
