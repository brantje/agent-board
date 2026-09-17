package opencode

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestDelegationPermissionV2MapsCurrentPayload(t *testing.T) {
	var replies []permissionReply
	native := delegationPermissionClient(t, nil, &replies)
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}
	metadata := delegationPermissionMetadata{
		Tool: delegationToolName, TargetAgentID: "agent-2", Task: "inspect scheduler ownership", CallID: "call-v2",
	}
	event := client.Event{Type: "permission.v2.asked", Properties: mustJSON(t, map[string]any{
		"id": "per-v2", "sessionID": "ses-1", "action": delegationPermissionName,
		"resources": []string{"call-v2"}, "save": []string{}, "metadata": metadata,
		"source": map[string]any{"type": "tool", "messageID": "msg-v2", "callID": "call-v2"},
	})}

	if err := tracker.Handle(t.Context(), event, native, "ses-1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("requests=%+v", requester.requests)
	}
	got := requester.requests[0]
	if got.TargetAgentID != "agent-2" || got.Task != "inspect scheduler ownership" || got.RequestKey != "call-v2" {
		t.Fatalf("request=%+v", got)
	}
	if len(replies) != 1 || replies[0].requestID != "per-v2" || replies[0].response != "once" {
		t.Fatalf("permission replies=%+v", replies)
	}
}

func TestDelegationPermissionV2WithoutToolSourceIsRejected(t *testing.T) {
	var replies []permissionReply
	native := delegationPermissionClient(t, nil, &replies)
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}
	metadata := delegationPermissionMetadata{
		Tool: delegationToolName, TargetAgentID: "agent-2", Task: "task", CallID: "call-v2",
	}
	event := client.Event{Type: "permission.v2.asked", Properties: mustJSON(t, map[string]any{
		"id": "per-v2", "sessionID": "ses-1", "action": delegationPermissionName,
		"resources": []string{"call-v2"}, "metadata": metadata,
		"source": map[string]any{"type": "user", "callID": "call-v2"},
	})}

	if err := tracker.Handle(t.Context(), event, native, "ses-1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 0 {
		t.Fatalf("untrusted V2 source created delegation request: %+v", requester.requests)
	}
	if len(replies) != 1 || replies[0].requestID != "per-v2" || replies[0].response != "reject" {
		t.Fatalf("permission replies=%+v", replies)
	}
}
