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
	issueStatusToolName   = "set_issue_status"
	issueStatusToolSource = `import { tool } from "@opencode-ai/plugin"

export default tool({
  description: "Update the Board status of the current Agent Board Issue. This tool is scoped to the current Run and cannot target another Issue.",
  args: {
    status: tool.schema.enum(["BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE"]).describe("New Board status for the current Issue"),
  },
  async execute({ status }) {
    return "Emitted Board status change request: " + status
  },
})
`
)

func issueStatusServeCommand(host, port string, _ map[string]string) []string {
	const script = `set -eu
config_home="${XDG_CONFIG_HOME:?XDG_CONFIG_HOME is required}"
tool_dir="$config_home/opencode/tools"
mkdir -p "$tool_dir"
printf '%s' "$3" > "$tool_dir/set_issue_status.ts"
exec opencode serve --hostname "$1" --port "$2"`
	return []string{"sh", "-c", script, "agent-board-opencode", host, port, issueStatusToolSource}
}

type issueStatusToolTracker struct {
	seen map[string]struct{}
}

func newIssueStatusToolTracker() *issueStatusToolTracker {
	return &issueStatusToolTracker{seen: make(map[string]struct{})}
}

func (t *issueStatusToolTracker) Handle(ctx context.Context, event client.Event, sessionID string, updater engine.IssueStatusUpdater) error {
	if event.Type != "message.part.updated" {
		return nil
	}
	var update struct {
		SessionID string          `json:"sessionID"`
		Part      json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(event.Properties, &update); err != nil {
		return fmt.Errorf("opencode engine: decode Issue status tool event: %w", err)
	}
	if update.SessionID != sessionID || len(update.Part) == 0 {
		return nil
	}
	var part struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
		Type      string `json:"type"`
		Tool      string `json:"tool"`
		State     struct {
			Status string          `json:"status"`
			Input  json.RawMessage `json:"input,omitempty"`
		} `json:"state"`
	}
	if err := json.Unmarshal(update.Part, &part); err != nil {
		return fmt.Errorf("opencode engine: decode Issue status tool part: %w", err)
	}
	if part.SessionID != "" && part.SessionID != sessionID {
		return nil
	}
	if part.Type != "tool" || part.Tool != issueStatusToolName || part.State.Status != "completed" {
		return nil
	}
	if strings.TrimSpace(part.ID) == "" {
		return fmt.Errorf("opencode engine: Issue status tool completion is missing part id")
	}
	key := part.ID + "/completed"
	if _, duplicate := t.seen[key]; duplicate {
		return nil
	}
	input := decodeToolInput(part.State.Input)
	status, ok := input["status"].(string)
	status = strings.TrimSpace(status)
	if !ok || status == "" {
		return fmt.Errorf("opencode engine: Issue status tool requires status")
	}
	if updater == nil {
		return fmt.Errorf("opencode engine: Issue status capability is unavailable")
	}
	if err := updater.SetStatus(ctx, status); err != nil {
		return fmt.Errorf("opencode engine: set Issue status: %w", err)
	}
	t.seen[key] = struct{}{}
	return nil
}
