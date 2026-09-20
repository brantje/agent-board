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
    return JSON.stringify({ status: "accepted", targetAgentId })
  },
})
`
)

func openCodeServeCommand(host, port string, issueStatusEnabled, issueCommentEnabled, delegationEnabled bool) []string {
	statusSource := ""
	if issueStatusEnabled {
		statusSource = issueStatusToolSource
	}
	commentSource := ""
	if issueCommentEnabled {
		commentSource = issueCommentToolSource
	}
	delegationSource := ""
	if delegationEnabled {
		delegationSource = delegationToolSource
	}
	const script = `set -eu
config_home="${XDG_CONFIG_HOME:?XDG_CONFIG_HOME is required}"
tool_dir="$config_home/opencode/tools"
mkdir -p "$tool_dir"
rm -f "$tool_dir/set_issue_status.ts" "$tool_dir/publish_issue_comment.ts" "$tool_dir/delegate_task.ts"
if [ -n "$3" ]; then printf '%s' "$3" > "$tool_dir/set_issue_status.ts"; fi
if [ -n "$4" ]; then printf '%s' "$4" > "$tool_dir/publish_issue_comment.ts"; fi
if [ -n "$5" ]; then printf '%s' "$5" > "$tool_dir/delegate_task.ts"; fi
exec opencode serve --hostname "$1" --port "$2"`
	return []string{"sh", "-c", script, "agent-board-opencode", host, port, statusSource, commentSource, delegationSource}
}

func openCodeServeCommandWithDiscussion(host, port string, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled bool) []string {
	if !issueDiscussionEnabled {
		return openCodeServeCommand(host, port, issueStatusEnabled, issueCommentEnabled, delegationEnabled)
	}
	statusSource := ""
	if issueStatusEnabled {
		statusSource = issueStatusToolSource
	}
	commentSource := ""
	if issueCommentEnabled {
		commentSource = issueCommentToolSource
	}
	discussionSource := issueDiscussionToolSource
	delegationSource := ""
	if delegationEnabled {
		delegationSource = delegationToolSource
	}
	const script = `set -eu
config_home="${XDG_CONFIG_HOME:?XDG_CONFIG_HOME is required}"
tool_dir="$config_home/opencode/tools"
mkdir -p "$tool_dir"
rm -f "$tool_dir/set_issue_status.ts" "$tool_dir/publish_issue_comment.ts" "$tool_dir/read_issue_discussion.ts" "$tool_dir/delegate_task.ts"
if [ -n "$3" ]; then printf '%s' "$3" > "$tool_dir/set_issue_status.ts"; fi
if [ -n "$4" ]; then printf '%s' "$4" > "$tool_dir/publish_issue_comment.ts"; fi
printf '%s' "$5" > "$tool_dir/read_issue_discussion.ts"
if [ -n "$6" ]; then printf '%s' "$6" > "$tool_dir/delegate_task.ts"; fi
exec opencode serve --hostname "$1" --port "$2"`
	return []string{"sh", "-c", script, "agent-board-opencode", host, port, statusSource, commentSource, discussionSource, delegationSource}
}

type delegationPermissionMetadata struct {
	Tool          string `json:"tool"`
	TargetAgentID string `json:"targetAgentId"`
	Task          string `json:"task"`
	CallID        string `json:"callID"`
}

type delegationToolPart struct {
	ID        string `json:"id"`
	CallID    string `json:"callID,omitempty"`
	SessionID string `json:"sessionID"`
	Type      string `json:"type"`
	Tool      string `json:"tool"`
	State     struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input,omitempty"`
	} `json:"state"`
}

type acceptedDelegation struct {
	request    engine.DelegationRequest
	delegation engine.Delegation
}

type delegationToolTracker struct {
	accepted map[string]acceptedDelegation
	handoff  *engine.Delegation
}

func newDelegationToolTracker() *delegationToolTracker {
	return &delegationToolTracker{accepted: make(map[string]acceptedDelegation)}
}

func (t *delegationToolTracker) Handle(ctx context.Context, event client.Event, native *client.Client, sessionID string, requester engine.DelegationRequester) error {
	if t == nil {
		return fmt.Errorf("opencode engine: delegation tracker is unavailable")
	}
	if t.handoff != nil {
		return engine.NewDelegationHandoff(*t.handoff)
	}
	switch event.Type {
	case "permission.asked", "permission.v2.asked":
		permission, err := decodeDelegationPermission(event)
		if err != nil {
			return fmt.Errorf("opencode engine: decode delegation permission event: %w", err)
		}
		return t.applyPermission(ctx, native, sessionID, permission, requester)
	case "message.part.updated":
		return t.handleTerminalPart(event, sessionID)
	default:
		return nil
	}
}

func decodeDelegationPermission(event client.Event) (client.PermissionRequest, error) {
	if event.Type == "permission.asked" {
		var permission client.PermissionRequest
		if err := json.Unmarshal(event.Properties, &permission); err != nil {
			return client.PermissionRequest{}, err
		}
		return permission, nil
	}

	var current struct {
		ID        string          `json:"id"`
		SessionID string          `json:"sessionID"`
		Action    string          `json:"action"`
		Resources []string        `json:"resources"`
		Save      []string        `json:"save"`
		Metadata  json.RawMessage `json:"metadata"`
		Source    *struct {
			Type      string `json:"type"`
			MessageID string `json:"messageID"`
			CallID    string `json:"callID"`
		} `json:"source"`
	}
	if err := json.Unmarshal(event.Properties, &current); err != nil {
		return client.PermissionRequest{}, err
	}
	permission := client.PermissionRequest{
		ID:         current.ID,
		SessionID:  current.SessionID,
		Permission: current.Action,
		Patterns:   current.Resources,
		Metadata:   current.Metadata,
		Always:     current.Save,
	}
	if current.Source != nil && current.Source.Type == "tool" {
		permission.Tool = &struct {
			MessageID string `json:"messageID"`
			CallID    string `json:"callID"`
		}{MessageID: current.Source.MessageID, CallID: current.Source.CallID}
	}
	return permission, nil
}

func (t *delegationToolTracker) Reconcile(ctx context.Context, native *client.Client, sessionID string, requester engine.DelegationRequester) error {
	if t == nil {
		return fmt.Errorf("opencode engine: delegation tracker is unavailable")
	}
	if t.handoff != nil {
		return engine.NewDelegationHandoff(*t.handoff)
	}
	if requester == nil {
		return nil
	}
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
	if err := t.reconcileTerminalParts(ctx, native, sessionID, requester); err != nil {
		return err
	}
	if t.handoff != nil {
		return engine.NewDelegationHandoff(*t.handoff)
	}
	return nil
}

func (t *delegationToolTracker) reconcileTerminalParts(ctx context.Context, native *client.Client, sessionID string, requester engine.DelegationRequester) error {
	messages, err := native.ListMessages(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("opencode engine: list messages for delegation reconciliation: %w", err)
	}
	for _, message := range messages {
		for _, raw := range message.Parts {
			var part delegationToolPart
			if err := json.Unmarshal(raw, &part); err != nil {
				return fmt.Errorf("opencode engine: decode delegation tool part: %w", err)
			}
			request, terminalStatus, matched, err := delegationRequestFromTerminalPart(part, sessionID)
			if err != nil {
				return err
			}
			if !matched {
				continue
			}
			if accepted, ok := t.accepted[request.RequestKey]; ok {
				if !sameDelegationRequest(accepted.request, request) {
					return fmt.Errorf("opencode engine: terminal delegation tool input changed after canonical acceptance")
				}
				t.handoff = &accepted.delegation
				return nil
			}
			switch terminalStatus {
			case "completed":
				delegation, err := requester.Delegate(ctx, request)
				if err != nil {
					return fmt.Errorf("opencode engine: reconcile completed delegation: %w", err)
				}
				t.accepted[request.RequestKey] = acceptedDelegation{request: request, delegation: delegation}
				t.handoff = &delegation
				return nil
			case "error":
				resolver, ok := requester.(engine.AcceptedDelegationResolver)
				if !ok {
					return fmt.Errorf("opencode engine: accepted delegation recovery resolver is unavailable")
				}
				delegation, found, err := resolver.ResolveAcceptedDelegation(ctx, request)
				if err != nil {
					return fmt.Errorf("opencode engine: prove accepted errored delegation: %w", err)
				}
				if !found {
					// A native error is not authority to create work. Without a
					// durable canonical request this remains a rejected tool call.
					continue
				}
				t.accepted[request.RequestKey] = acceptedDelegation{request: request, delegation: delegation}
				t.handoff = &delegation
				return nil
			}
		}
	}
	return nil
}

func (t *delegationToolTracker) handleTerminalPart(event client.Event, sessionID string) error {
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
	var part delegationToolPart
	if err := json.Unmarshal(update.Part, &part); err != nil {
		return fmt.Errorf("opencode engine: decode delegation tool part: %w", err)
	}
	if strings.TrimSpace(part.CallID) == "" {
		// A live completion with no stable call identity cannot be tied back to
		// a canonical permission/request. Ignore it here; durable recovery uses
		// the strict parser below and fails closed if identity is unavailable.
		return nil
	}
	request, _, matched, err := delegationRequestFromTerminalPart(part, sessionID)
	if err != nil || !matched {
		return err
	}
	accepted, ok := t.accepted[request.RequestKey]
	if !ok {
		// Live completion alone is not authority to create work. Recovery may
		// reconstruct only through the canonical idempotent request path.
		return nil
	}
	if !sameDelegationRequest(accepted.request, request) {
		return fmt.Errorf("opencode engine: terminal delegation tool input changed after canonical acceptance")
	}
	t.handoff = &accepted.delegation
	return nil
}

func delegationRequestFromTerminalPart(part delegationToolPart, sessionID string) (engine.DelegationRequest, string, bool, error) {
	if part.SessionID != "" && part.SessionID != sessionID {
		return engine.DelegationRequest{}, "", false, nil
	}
	if part.Type != "tool" || part.Tool != delegationToolName {
		return engine.DelegationRequest{}, "", false, nil
	}
	status := strings.TrimSpace(part.State.Status)
	if status != "completed" && status != "error" {
		return engine.DelegationRequest{}, "", false, nil
	}
	callID := strings.TrimSpace(part.CallID)
	if callID == "" {
		return engine.DelegationRequest{}, "", false, fmt.Errorf("opencode engine: terminal delegation tool is missing call id")
	}
	input := decodeToolInput(part.State.Input)
	targetAgentID, _ := input["targetAgentId"].(string)
	task, _ := input["task"].(string)
	targetAgentID = strings.TrimSpace(targetAgentID)
	task = strings.TrimSpace(task)
	if targetAgentID == "" || task == "" {
		return engine.DelegationRequest{}, "", false, fmt.Errorf("opencode engine: terminal delegation tool input is invalid")
	}
	return engine.DelegationRequest{TargetAgentID: targetAgentID, Task: task, RequestKey: callID}, status, true, nil
}

func sameDelegationRequest(left, right engine.DelegationRequest) bool {
	return left.TargetAgentID == right.TargetAgentID && left.Task == right.Task && left.RequestKey == right.RequestKey
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
	request := engine.DelegationRequest{
		TargetAgentID: metadata.TargetAgentID,
		Task:          metadata.Task,
		RequestKey:    callID,
	}
	delegation, err := requester.Delegate(ctx, request)
	if err != nil {
		return t.rejectPermission(ctx, native, sessionID, requestID, err)
	}
	if err := native.ReplyPermission(ctx, sessionID, requestID, "once"); err != nil {
		return fmt.Errorf("opencode engine: approve delegation permission: %w", err)
	}
	t.accepted[callID] = acceptedDelegation{request: request, delegation: delegation}
	return nil
}

func (t *delegationToolTracker) rejectPermission(ctx context.Context, native *client.Client, sessionID, requestID string, cause error) error {
	if err := native.ReplyPermission(ctx, sessionID, requestID, "reject"); err != nil {
		return errors.Join(fmt.Errorf("opencode engine: reject delegation permission: %w", err), cause)
	}
	return nil
}
