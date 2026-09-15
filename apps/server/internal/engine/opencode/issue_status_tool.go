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

type issueStatusToolPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Type      string `json:"type"`
	Tool      string `json:"tool"`
	State     struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input,omitempty"`
	} `json:"state"`
}

type issueStatusRecoveryUpdater interface {
	SetRecoveredStatus(context.Context, string) error
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
	part, err := decodeIssueStatusToolPart(update.Part)
	if err != nil {
		return err
	}
	return t.applyPart(ctx, part, sessionID, updater, false)
}

func (t *issueStatusToolTracker) Reconcile(ctx context.Context, native *client.Client, sessionID string, updater engine.IssueStatusUpdater) error {
	return t.reconcileDurable(ctx, native, sessionID, updater)
}

// ReconcileAttach restores the final durable Board intent from a recovered
// native session without replaying an already-applied historical status sequence.
func (t *issueStatusToolTracker) ReconcileAttach(ctx context.Context, native *client.Client, sessionID string, updater engine.IssueStatusUpdater) error {
	return t.reconcileDurable(ctx, native, sessionID, updater)
}

func (t *issueStatusToolTracker) reconcileDurable(ctx context.Context, native *client.Client, sessionID string, updater engine.IssueStatusUpdater) error {
	parts, err := durableIssueStatusToolParts(ctx, native, sessionID)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return nil
	}
	// Board status is a current value, not an event stream to reconstruct. Older
	// durable tool intents are superseded by the final durable intent from this
	// session, whether or not Agent Board observed them live.
	for _, part := range parts[:len(parts)-1] {
		if partID := strings.TrimSpace(part.ID); partID != "" {
			t.seen[partID] = struct{}{}
		}
	}
	return t.applyPart(ctx, parts[len(parts)-1], sessionID, updater, true)
}

func durableIssueStatusToolParts(ctx context.Context, native *client.Client, sessionID string) ([]issueStatusToolPart, error) {
	if native == nil {
		return nil, fmt.Errorf("opencode engine: native client is required for Issue status reconciliation")
	}
	messages, err := native.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("opencode engine: list messages for Issue status reconciliation: %w", err)
	}
	parts := make([]issueStatusToolPart, 0)
	for _, message := range messages {
		var info struct {
			SessionID string `json:"sessionID"`
		}
		if len(message.Info) > 0 {
			if err := json.Unmarshal(message.Info, &info); err != nil {
				return nil, fmt.Errorf("opencode engine: decode Issue status message info: %w", err)
			}
			if info.SessionID != "" && info.SessionID != sessionID {
				continue
			}
		}
		for _, raw := range message.Parts {
			part, err := decodeIssueStatusToolPart(raw)
			if err != nil {
				return nil, err
			}
			if part.Type == "tool" && part.Tool == issueStatusToolName && part.State.Status == "completed" &&
				(part.SessionID == "" || part.SessionID == sessionID) {
				parts = append(parts, part)
			}
		}
	}
	return parts, nil
}

func decodeIssueStatusToolPart(raw json.RawMessage) (issueStatusToolPart, error) {
	var part issueStatusToolPart
	if err := json.Unmarshal(raw, &part); err != nil {
		return issueStatusToolPart{}, fmt.Errorf("opencode engine: decode Issue status tool part: %w", err)
	}
	return part, nil
}

func (t *issueStatusToolTracker) applyPart(ctx context.Context, part issueStatusToolPart, sessionID string, updater engine.IssueStatusUpdater, recovery bool) error {
	if part.SessionID != "" && part.SessionID != sessionID {
		return nil
	}
	if part.Type != "tool" || part.Tool != issueStatusToolName || part.State.Status != "completed" {
		return nil
	}
	partID := strings.TrimSpace(part.ID)
	if partID == "" {
		return fmt.Errorf("opencode engine: Issue status tool completion is missing part id")
	}
	if _, duplicate := t.seen[partID]; duplicate {
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
	var err error
	if recovery {
		recoveryUpdater, ok := updater.(issueStatusRecoveryUpdater)
		if !ok {
			return fmt.Errorf("opencode engine: recovered Issue status capability is unavailable")
		}
		err = recoveryUpdater.SetRecoveredStatus(ctx, status)
	} else {
		err = updater.SetStatus(ctx, status)
	}
	if err != nil {
		return fmt.Errorf("opencode engine: set Issue status: %w", err)
	}
	t.seen[partID] = struct{}{}
	return nil
}
