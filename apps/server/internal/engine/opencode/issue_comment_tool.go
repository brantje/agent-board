package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	issueCommentToolName   = "publish_issue_comment"
	issueCommentToolSource = `import { tool } from "@opencode-ai/plugin"

export default tool({
  description: "Publish a concise durable comment on the current Agent Board Issue. Use it deliberately for findings, results, handoffs, non-blocking collaboration questions, or pointers to existing Run/Review evidence. To request focused work, pass at most one stable typed target in mentionTargets; plain @name text never routes work. Do not copy raw logs, command/test output, file contents, progress chatter, or hidden reasoning. Use the native Question capability for blocking human input.",
  args: {
    body: tool.schema.string().describe("Concise user-visible Issue comment"),
    mentionTargets: tool.schema.array(tool.schema.object({ type: tool.schema.enum(["AGENT", "SQUAD"]), id: tool.schema.string() })).optional().describe("Optional single stable Agent or Squad target to mention structurally and request focused work from"),
  },
  async execute({ body, mentionAgentIds }) {
    return JSON.stringify({ status: "emitted", mentionAgentIds: mentionAgentIds ?? [] })
  },
})
`
)

type issueCommentToolPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Type      string `json:"type"`
	Tool      string `json:"tool"`
	State     struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input,omitempty"`
	} `json:"state"`
}

type issueCommentToolTracker struct {
	seen map[string]struct{}
}

func newIssueCommentToolTracker() *issueCommentToolTracker {
	return &issueCommentToolTracker{seen: make(map[string]struct{})}
}

func (t *issueCommentToolTracker) Handle(ctx context.Context, event client.Event, sessionID string, publisher engine.IssueCommentPublisher) error {
	if event.Type != "message.part.updated" {
		return nil
	}
	var update struct {
		SessionID string          `json:"sessionID"`
		Part      json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(event.Properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode Issue comment tool event: %w", err)
	}
	if update.SessionID != sessionID || len(update.Part) == 0 {
		return nil
	}
	var part issueCommentToolPart
	if err := json.Unmarshal(update.Part, &part); err != nil {
		return fmt.Errorf("opencode engine: decode Issue comment tool part: %w", err)
	}
	return t.applyPart(ctx, part, sessionID, publisher)
}

func (t *issueCommentToolTracker) Reconcile(ctx context.Context, native *client.Client, sessionID string, publisher engine.IssueCommentPublisher) error {
	if publisher == nil {
		return nil
	}
	if native == nil {
		return fmt.Errorf("opencode engine: native client is required for Issue comment reconciliation")
	}
	messages, err := native.ListMessages(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("opencode engine: list messages for Issue comment reconciliation: %w", err)
	}
	for _, message := range messages {
		var info struct {
			SessionID string `json:"sessionID"`
		}
		if len(message.Info) > 0 {
			if err := json.Unmarshal(message.Info, &info); err != nil {
				return fmt.Errorf("opencode engine: decode Issue comment message info: %w", err)
			}
			if info.SessionID != "" && info.SessionID != sessionID {
				continue
			}
		}
		for _, raw := range message.Parts {
			var part issueCommentToolPart
			if err := json.Unmarshal(raw, &part); err != nil {
				return fmt.Errorf("opencode engine: decode Issue comment tool part: %w", err)
			}
			if err := t.applyPart(ctx, part, sessionID, publisher); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *issueCommentToolTracker) applyPart(ctx context.Context, part issueCommentToolPart, sessionID string, publisher engine.IssueCommentPublisher) error {
	if part.SessionID != "" && part.SessionID != sessionID {
		return nil
	}
	if part.Type != "tool" || part.Tool != issueCommentToolName || strings.TrimSpace(part.State.Status) != "completed" {
		return nil
	}
	partID := strings.TrimSpace(part.ID)
	if partID == "" {
		return fmt.Errorf("opencode engine: Issue comment tool completion is missing part id")
	}
	if _, duplicate := t.seen[partID]; duplicate {
		return nil
	}
	if publisher == nil {
		return fmt.Errorf("opencode engine: Issue comment capability is unavailable")
	}
	input := decodeToolInput(part.State.Input)
	body, _ := input["body"].(string)
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("opencode engine: Issue comment tool requires body")
	}
	mentionTargets, err := issueCommentTargets(input["mentionTargets"])
	if err != nil {
		return err
	}
	var mentionAgentIDs []string
	if input["mentionTargets"] == nil {
		mentionAgentIDs, err = issueCommentMentionAgentIDs(input["mentionAgentIds"])
		if err != nil {
			return err
		}
	}
	published, err := publisher.PublishIssueComment(ctx, engine.IssueCommentPublishRequest{
		Body: body, RequestKey: partID, MentionTargets: mentionTargets, MentionAgentIDs: mentionAgentIDs,
	})
	if err != nil {
		return fmt.Errorf("opencode engine: publish Issue comment: %w", err)
	}
	t.seen[partID] = struct{}{}
	if published.Delegation != nil {
		return engine.NewDelegationHandoff(*published.Delegation)
	}
	return nil
}
func issueCommentMentionAgentIDs(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("opencode engine: Issue comment mentionAgentIds must be an array")
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		id, ok := value.(string)
		id = strings.TrimSpace(id)
		if !ok || id == "" {
			return nil, fmt.Errorf("opencode engine: Issue comment mentionAgentIds must contain non-empty Agent IDs")
		}
		result = append(result, id)
	}
	return result, nil
}

func issueCommentTargets(raw any) ([]store.IssueCommentTarget, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("opencode engine: Issue comment mentionTargets must be an array")
	}
	result := make([]store.IssueCommentTarget, 0, len(values))
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("opencode engine: Issue comment mentionTargets must contain objects")
		}
		typeValue, typeOK := object["type"].(string)
		id, idOK := object["id"].(string)
		typeValue = strings.TrimSpace(typeValue)
		id = strings.TrimSpace(id)
		if !typeOK || !idOK || (typeValue != store.IssueCommentTargetTypeAgent && typeValue != store.IssueCommentTargetTypeSquad) || id == "" {
			return nil, fmt.Errorf("opencode engine: Issue comment mentionTargets must contain valid typed targets")
		}
		result = append(result, store.IssueCommentTarget{Type: typeValue, ID: id})
	}
	return result, nil
}
