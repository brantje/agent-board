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
	delegationToolName       = "delegate_task"
	delegationPermissionName = "agent_board_delegate"
	delegationToolSource     = `import { tool } from "@opencode-ai/plugin"

export default tool({
  description: "Delegate one bounded task on the current Agent Board Issue to another Agent. Parent Issue/Run identity is server-owned.",
  args: {
    targetAgentId: tool.schema.string().describe("Target Agent ID"),
    task: tool.schema.string().describe("Bounded task for the target Agent"),
  },
  async execute({ targetAgentId, task }, context) {
    const callID = String(context.callID ?? "").trim()
    if (!callID) throw new Error("delegate_task requires a stable call identity")
    await context.ask({
      permission: "agent_board_delegate",
      patterns: [callID],
      always: [],
      metadata: {
        tool: "delegate_task",
        targetAgentId,
        task,
        callID,
      },
    })
    return "Delegation accepted for Agent " + targetAgentId
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

type delegationPermissionMetadata struct {
	Tool          string `json:"tool"`
	TargetAgentID string `json:"targetAgentId"`
	Task          string `json:"task"`
	CallID        string `json:"callID"`
}

type delegationToolTracker struct {
	accepted map[string]struct{}
}

func newDelegationToolTracker() *delegationToolTracker {
	return &delegationToolTracker{accepted: make(map[string]struct{})}
}

func (t *delegationToolTracker) Handle(ctx context.Context, event client.Event, native *client.Client, sessionID string, requester engine.DelegationRequester) error {
	if event.Type != "permission.asked" && event.Type != "permission.v2.asked" {
		return nil
	}
	var permission client.PermissionRequest
	if err := json.Unmarshal(event.Properties, &permission); err != nil {
		return fmt.Errorf("opencode engine: decode delegation permission event: %w", err)
	}
	return t.applyPermission(ctx, native, sessionID, permission, requester)
}

func (t *delegationToolTracker) Reconcile(ctx context.Context, native *client.Client, sessionID string, requester engine.DelegationRequester) error {
	if native == nil {
		return fmt.Errorf("opencode engine: native client is required for delegation reconciliation")
	}
	permissions, err := native.ListPermissions(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("opencode engine: list delegation permissions: %w", err)
	}
	for _, permission := range permissions {
		if err := t.applyPermission(ctx, native, sessionID, permission, requester); err != nil {
			return err
		}
	}
	return nil
}

func (t *delegationToolTracker) applyPermission(ctx context.Context, native *client.Client, sessionID string, permission client.PermissionRequest, requester engine.DelegationRequester) error {
	if permission.SessionID != sessionID || permission.Permission != delegationPermissionName {
		return nil
	}
	if native == nil {
		return fmt.Errorf("opencode engine: native client is required for delegation permission")
	}
	requestID := strings.TrimSpace(permission.ID)
	if requestID == "" {
		return fmt.Errorf("opencode engine: delegation permission is missing request id")
	}
	callID := ""
	if permission.Tool != nil {
		callID = strings.TrimSpace(permission.Tool.CallID)
	}
	if callID == "" {
		return t.rejectPermission(ctx, native, sessionID, requestID, fmt.Errorf("delegation permission is missing tool call id"))
	}
	var metadata delegationPermissionMetadata
	if len(permission.Metadata) == 0 || json.Unmarshal(permission.Metadata, &metadata) != nil {
		return t.rejectPermission(ctx, native, sessionID, requestID, fmt.Errorf("delegation permission metadata is invalid"))
	}
	metadata.Tool = strings.TrimSpace(metadata.Tool)
	metadata.TargetAgentID = strings.TrimSpace(metadata.TargetAgentID)
	metadata.Task = strings.TrimSpace(metadata.Task)
	metadata.CallID = strings.TrimSpace(metadata.CallID)
	if metadata.Tool != delegationToolName || metadata.TargetAgentID == "" || metadata.Task == "" || metadata.CallID != callID {
		return t.rejectPermission(ctx, native, sessionID, requestID, fmt.Errorf("delegation permission metadata is inconsistent"))
	}
	if _, duplicate := t.accepted[callID]; duplicate {
		return native.ReplyPermission(ctx, sessionID, requestID, "once")
	}
	if requester == nil {
		return t.rejectPermission(ctx, native, sessionID, requestID, fmt.Errorf("delegation capability is unavailable"))
	}
	if _, err := requester.Delegate(ctx, engine.DelegationRequest{
		TargetAgentID: metadata.TargetAgentID,
		Task:          metadata.Task,
		RequestKey:    callID,
	}); err != nil {
		return t.rejectPermission(ctx, native, sessionID, requestID, err)
	}
	if err := native.ReplyPermission(ctx, sessionID, requestID, "once"); err != nil {
		return fmt.Errorf("opencode engine: approve delegation permission: %w", err)
	}
	t.accepted[callID] = struct{}{}
	return nil
}

func (t *delegationToolTracker) rejectPermission(ctx context.Context, native *client.Client, sessionID, requestID string, cause error) error {
	if err := native.ReplyPermission(ctx, sessionID, requestID, "reject"); err != nil {
		return errors.Join(fmt.Errorf("opencode engine: reject delegation permission: %w", err), cause)
	}
	return nil
}
