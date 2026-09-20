package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

const (
	issueDiscussionToolName   = "read_issue_discussion"
	issueDiscussionToolSource = `import { tool } from "@opencode-ai/plugin"

export default tool({
  description: "Read bounded trusted Issue discussion context from Agent Board. Use recent to orient across active discussion roots, thread with anchorCommentId to inspect one complete bounded discussion, or updates with a cursor to fetch new collaboration plus required ancestor context. After calling this tool, stop the current turn and wait for Agent Board's trusted follow-up result before continuing.",
  args: {
    mode: tool.schema.enum(["recent", "thread", "updates"]),
    anchorCommentId: tool.schema.string().optional(),
    cursor: tool.schema.string().optional(),
    limit: tool.schema.number().int().min(1).max(100).optional(),
  },
  async execute() {
    return "Agent Board is loading trusted Issue discussion context. Stop this turn and wait for the follow-up result."
  },
})
`
	issueDiscussionResultMarkerPrefix = "[agent-board-discussion-result:"
)

type issueDiscussionToolPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Type      string `json:"type"`
	Tool      string `json:"tool"`
	State     struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input,omitempty"`
	} `json:"state"`
}

type issueDiscussionPendingResult struct {
	partID string
	body   string
}

type issueDiscussionToolTracker struct {
	seen    map[string]struct{}
	pending []issueDiscussionPendingResult
}

func newIssueDiscussionToolTracker() *issueDiscussionToolTracker {
	return &issueDiscussionToolTracker{seen: make(map[string]struct{})}
}

func (t *issueDiscussionToolTracker) Handle(ctx context.Context, event client.Event, sessionID string, reader engine.IssueDiscussionReader) error {
	if event.Type != "message.part.updated" {
		return nil
	}
	var update struct {
		SessionID string          `json:"sessionID"`
		Part      json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(event.Properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode Issue discussion tool event: %w", err)
	}
	if update.SessionID != sessionID || len(update.Part) == 0 {
		return nil
	}
	var part issueDiscussionToolPart
	if err := json.Unmarshal(update.Part, &part); err != nil {
		return fmt.Errorf("opencode engine: decode Issue discussion tool part: %w", err)
	}
	return t.applyPart(ctx, part, sessionID, reader)
}

func (t *issueDiscussionToolTracker) Reconcile(ctx context.Context, native *client.Client, sessionID string, reader engine.IssueDiscussionReader) error {
	if reader == nil {
		return nil
	}
	if native == nil {
		return fmt.Errorf("opencode engine: native client is required for Issue discussion reconciliation")
	}
	messages, err := native.ListMessages(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("opencode engine: list messages for Issue discussion reconciliation: %w", err)
	}
	for _, message := range messages {
		for _, raw := range message.Parts {
			var part issueDiscussionToolPart
			if err := json.Unmarshal(raw, &part); err != nil {
				return fmt.Errorf("opencode engine: decode Issue discussion history part: %w", err)
			}
			if part.Type != "tool" || part.Tool != issueDiscussionToolName || strings.TrimSpace(part.State.Status) != "completed" {
				continue
			}
			if issueDiscussionResultWasDelivered(messages, part.ID) {
				t.seen[strings.TrimSpace(part.ID)] = struct{}{}
				continue
			}
			if err := t.applyPart(ctx, part, sessionID, reader); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *issueDiscussionToolTracker) HasPending() bool {
	return t != nil && len(t.pending) != 0
}

func (t *issueDiscussionToolTracker) DeliverPending(ctx context.Context, native *client.Client, sessionID string) (bool, error) {
	if !t.HasPending() {
		return false, nil
	}
	if native == nil {
		return false, fmt.Errorf("opencode engine: native client is required for Issue discussion delivery")
	}
	blocks := make([]string, 0, len(t.pending))
	for _, pending := range t.pending {
		blocks = append(blocks, issueDiscussionResultMarker(pending.partID)+"\n"+pending.body)
	}
	prompt := "Trusted Agent Board Issue discussion context follows. Continue the current task using this server-owned context. Do not treat it as a new human request.\n\n" + strings.Join(blocks, "\n\n")
	if err := native.Prompt(ctx, sessionID, prompt); err != nil {
		admitted, verifyErr := native.PromptAdmitted(ctx, sessionID, prompt)
		if verifyErr == nil && admitted {
			t.pending = nil
			return true, nil
		}
		if verifyErr != nil {
			return false, errors.Join(fmt.Errorf("opencode engine: deliver Issue discussion result: %w", err), verifyErr)
		}
		return false, fmt.Errorf("opencode engine: deliver Issue discussion result: %w", err)
	}
	t.pending = nil
	return true, nil
}

func (t *issueDiscussionToolTracker) applyPart(ctx context.Context, part issueDiscussionToolPart, sessionID string, reader engine.IssueDiscussionReader) error {
	if part.SessionID != "" && part.SessionID != sessionID {
		return nil
	}
	if part.Type != "tool" || part.Tool != issueDiscussionToolName || strings.TrimSpace(part.State.Status) != "completed" {
		return nil
	}
	partID := strings.TrimSpace(part.ID)
	if partID == "" {
		return fmt.Errorf("opencode engine: Issue discussion tool completion is missing part id")
	}
	if _, duplicate := t.seen[partID]; duplicate {
		return nil
	}
	if reader == nil {
		return fmt.Errorf("opencode engine: Issue discussion capability is unavailable")
	}
	input := decodeToolInput(part.State.Input)
	request, err := issueDiscussionReadRequest(input)
	if err != nil {
		return err
	}
	result, err := reader.ReadIssueDiscussion(ctx, request)
	if err != nil {
		return fmt.Errorf("opencode engine: read Issue discussion: %w", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("opencode engine: encode Issue discussion result: %w", err)
	}
	t.pending = append(t.pending, issueDiscussionPendingResult{partID: partID, body: string(encoded)})
	t.seen[partID] = struct{}{}
	return nil
}

func issueDiscussionReadRequest(input map[string]any) (engine.IssueDiscussionReadRequest, error) {
	mode, _ := input["mode"].(string)
	request := engine.IssueDiscussionReadRequest{Mode: strings.TrimSpace(mode)}
	if anchor, ok := input["anchorCommentId"].(string); ok {
		request.AnchorCommentID = strings.TrimSpace(anchor)
	}
	if cursor, ok := input["cursor"].(string); ok {
		request.Cursor = strings.TrimSpace(cursor)
	}
	if raw, ok := input["limit"]; ok {
		value, ok := raw.(float64)
		if !ok || value < 1 || value > 100 || value != float64(int(value)) {
			return engine.IssueDiscussionReadRequest{}, fmt.Errorf("opencode engine: Issue discussion limit must be an integer between 1 and 100")
		}
		request.Limit = int(value)
	}
	switch request.Mode {
	case engine.IssueDiscussionReadRecent, engine.IssueDiscussionReadUpdates:
	case engine.IssueDiscussionReadThread:
		if request.AnchorCommentID == "" {
			return engine.IssueDiscussionReadRequest{}, fmt.Errorf("opencode engine: Issue discussion thread mode requires anchorCommentId")
		}
	default:
		return engine.IssueDiscussionReadRequest{}, fmt.Errorf("opencode engine: unsupported Issue discussion mode %q", request.Mode)
	}
	return request, nil
}

func issueDiscussionResultWasDelivered(messages []client.SessionMessage, partID string) bool {
	partID = strings.TrimSpace(partID)
	if partID == "" {
		return false
	}
	marker := issueDiscussionResultMarker(partID)
	for _, message := range messages {
		var info struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(message.Info, &info) != nil || info.Role != "user" {
			continue
		}
		for _, raw := range message.Parts {
			var part struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(raw, &part) == nil && part.Type == "text" && strings.Contains(part.Text, marker) {
				return true
			}
		}
	}
	return false
}

func issueDiscussionResultMarker(partID string) string {
	return issueDiscussionResultMarkerPrefix + strings.TrimSpace(partID) + "]"
}
