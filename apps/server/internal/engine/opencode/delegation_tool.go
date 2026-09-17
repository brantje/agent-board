package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

const (
	delegationToolName   = "delegate_task"
	delegationToolSource = `import { tool } from "@opencode-ai/plugin"

export default tool({
  description: "Delegate one bounded task on the current Agent Board Issue to another Agent. Parent Issue/Run identity is server-owned.",
  args: {
    targetAgentId: tool.schema.string().describe("Target Agent ID"),
    task: tool.schema.string().describe("Bounded task for the target Agent"),
  },
  async execute({ targetAgentId, task }) {
    return "Emitted delegation request for Agent " + targetAgentId + ": " + task
  },
})
`
)

func openCodeServeCommand(host, port string, issueStatusEnabled, delegationEnabled bool) []string {
	statusSource := ""
	if issueStatusEnabled {
		statusSource = issueStatusToolSource
	}
	delegationSource := ""
	if delegationEnabled {
		delegationSource = delegationToolSource
	}
	const script = `set -eu
config_home="${XDG_CONFIG_HOME:?XDG_CONFIG_HOME is required}"
tool_dir="$config_home/opencode/tools"
mkdir -p "$tool_dir"
rm -f "$tool_dir/set_issue_status.ts" "$tool_dir/delegate_task.ts"
if [ -n "$3" ]; then printf '%s' "$3" > "$tool_dir/set_issue_status.ts"; fi
if [ -n "$4" ]; then printf '%s' "$4" > "$tool_dir/delegate_task.ts"; fi
exec opencode serve --hostname "$1" --port "$2"`
	return []string{"sh", "-c", script, "agent-board-opencode", host, port, statusSource, delegationSource}
}

type delegationToolPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Type      string `json:"type"`
	Tool      string `json:"tool"`
	State     struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input,omitempty"`
	} `json:"state"`
}

type delegationToolTracker struct {
	seen map[string]struct{}
}

func newDelegationToolTracker() *delegationToolTracker {
	return &delegationToolTracker{seen: make(map[string]struct{})}
}

func (t *delegationToolTracker) Handle(ctx context.Context, event client.Event, sessionID string, requester engine.DelegationRequester) error {
	if event.Type != "message.part.updated" {
		return nil
	}
	var update struct {
		SessionID string          `json:"sessionID"`
		Part      json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(event.Properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode delegation tool event: %w", err)
	}
	if update.SessionID != sessionID || len(update.Part) == 0 {
		return nil
	}
	part, err := decodeDelegationToolPart(update.Part)
	if err != nil {
		return err
	}
	return t.applyPart(ctx, part, sessionID, requester)
}

func (t *delegationToolTracker) Reconcile(ctx context.Context, native *client.Client, sessionID string, requester engine.DelegationRequester) error {
	parts, err := durableDelegationToolParts(ctx, native, sessionID)
	if err != nil {
		return err
	}
	for _, part := range parts {
		if err := t.applyPart(ctx, part, sessionID, requester); err != nil {
			return err
		}
	}
	return nil
}

func durableDelegationToolParts(ctx context.Context, native *client.Client, sessionID string) ([]delegationToolPart, error) {
	if native == nil {
		return nil, fmt.Errorf("opencode engine: native client is required for delegation reconciliation")
	}
	messages, err := native.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("opencode engine: list messages for delegation reconciliation: %w", err)
	}
	parts := make([]delegationToolPart, 0)
	for _, message := range messages {
		var info struct {
			SessionID string `json:"sessionID"`
		}
		if len(message.Info) > 0 {
			if err := json.Unmarshal(message.Info, &info); err != nil {
				return nil, fmt.Errorf("opencode engine: decode delegation message info: %w", err)
			}
			if info.SessionID != "" && info.SessionID != sessionID {
				continue
			}
		}
		for _, raw := range message.Parts {
			part, err := decodeDelegationToolPart(raw)
			if err != nil {
				return nil, err
			}
			if part.Type == "tool" && part.Tool == delegationToolName && part.State.Status == "completed" &&
				(part.SessionID == "" || part.SessionID == sessionID) {
				parts = append(parts, part)
			}
		}
	}
	return parts, nil
}

func decodeDelegationToolPart(raw json.RawMessage) (delegationToolPart, error) {
	var part delegationToolPart
	if err := json.Unmarshal(raw, &part); err != nil {
		return delegationToolPart{}, fmt.Errorf("opencode engine: decode delegation tool part: %w", err)
	}
	return part, nil
}

func (t *delegationToolTracker) applyPart(ctx context.Context, part delegationToolPart, sessionID string, requester engine.DelegationRequester) error {
	if part.SessionID != "" && part.SessionID != sessionID {
		return nil
	}
	if part.Type != "tool" || part.Tool != delegationToolName || part.State.Status != "completed" {
		return nil
	}
	partID := strings.TrimSpace(part.ID)
	if partID == "" {
		return fmt.Errorf("opencode engine: delegation tool completion is missing part id")
	}
	if _, duplicate := t.seen[partID]; duplicate {
		return nil
	}
	input := decodeToolInput(part.State.Input)
	targetAgentID, ok := input["targetAgentId"].(string)
	targetAgentID = strings.TrimSpace(targetAgentID)
	if !ok || targetAgentID == "" {
		return fmt.Errorf("opencode engine: delegation tool requires targetAgentId")
	}
	task, ok := input["task"].(string)
	task = strings.TrimSpace(task)
	if !ok || task == "" {
		return fmt.Errorf("opencode engine: delegation tool requires task")
	}
	if requester == nil {
		return fmt.Errorf("opencode engine: delegation capability is unavailable")
	}
	if _, err := requester.Delegate(ctx, engine.DelegationRequest{
		TargetAgentID: targetAgentID,
		Task:          task,
		RequestKey:    partID,
	}); err != nil {
		return fmt.Errorf("opencode engine: request delegation: %w", err)
	}
	t.seen[partID] = struct{}{}
	return nil
}
