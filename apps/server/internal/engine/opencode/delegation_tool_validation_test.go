package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestDelegationPermissionTrackerRejectsMalformedAndUnavailableRequests(t *testing.T) {
	tracker := newDelegationToolTracker()
	ctx := t.Context()

	if err := tracker.Handle(ctx, client.Event{Type: "session.updated"}, nil, "ses_1", nil); err != nil {
		t.Fatalf("unrelated event returned error: %v", err)
	}
	if err := tracker.Handle(ctx, client.Event{Type: "permission.asked", Properties: json.RawMessage(`{`)}, nil, "ses_1", nil); err == nil || !strings.Contains(err.Error(), "decode delegation permission event") {
		t.Fatalf("malformed event error=%v", err)
	}

	var replies []permissionReply
	native := delegationPermissionClient(t, nil, &replies)
	foreign := delegationPermissionRequest(t, "per_foreign", "ses_other", "call_foreign", "agent-2", "task")
	if err := tracker.Handle(ctx, delegationPermissionEvent(t, foreign), native, "ses_1", &recordingDelegationRequester{}); err != nil {
		t.Fatalf("foreign session event returned error: %v", err)
	}
	if len(replies) != 0 {
		t.Fatalf("foreign request was replied to: %+v", replies)
	}

	missingCall := delegationPermissionRequest(t, "per_missing", "ses_1", "", "agent-2", "task")
	if err := tracker.Handle(ctx, delegationPermissionEvent(t, missingCall), native, "ses_1", &recordingDelegationRequester{}); err != nil {
		t.Fatalf("missing call id rejection returned error: %v", err)
	}
	if len(replies) != 1 || replies[0].response != "reject" {
		t.Fatalf("missing call id replies=%+v", replies)
	}

	mismatched := delegationPermissionRequest(t, "per_mismatch", "ses_1", "call_1", "agent-2", "task")
	mismatched.Metadata = mustJSON(t, delegationPermissionMetadata{
		Tool: delegationToolName, TargetAgentID: "agent-2", Task: "task", CallID: "call_other",
	})
	if err := tracker.Handle(ctx, delegationPermissionEvent(t, mismatched), native, "ses_1", &recordingDelegationRequester{}); err != nil {
		t.Fatalf("mismatched call id rejection returned error: %v", err)
	}
	if len(replies) != 2 || replies[1].response != "reject" {
		t.Fatalf("mismatched call id replies=%+v", replies)
	}

	unavailable := delegationPermissionRequest(t, "per_unavailable", "ses_1", "call_2", "agent-2", "task")
	if err := tracker.Handle(ctx, delegationPermissionEvent(t, unavailable), native, "ses_1", nil); err != nil {
		t.Fatalf("unavailable capability rejection returned error: %v", err)
	}
	if len(replies) != 3 || replies[2].response != "reject" {
		t.Fatalf("unavailable capability replies=%+v", replies)
	}
}

func TestDelegationPermissionTrackerRequiresNativeClient(t *testing.T) {
	tracker := newDelegationToolTracker()
	request := delegationPermissionRequest(t, "per_1", "ses_1", "call_1", "agent-2", "task")
	if err := tracker.Handle(t.Context(), delegationPermissionEvent(t, request), nil, "ses_1", &recordingDelegationRequester{}); err == nil || !strings.Contains(err.Error(), "native client is required") {
		t.Fatalf("nil native client error=%v", err)
	}
	if err := tracker.Reconcile(t.Context(), nil, "ses_1", &recordingDelegationRequester{}); err == nil || !strings.Contains(err.Error(), "native client is required") {
		t.Fatalf("nil reconcile client error=%v", err)
	}
}
