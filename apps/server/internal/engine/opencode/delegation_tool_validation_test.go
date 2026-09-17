package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestDelegationToolTrackerRejectsMalformedAndUnavailableCompletions(t *testing.T) {
	tracker := newDelegationToolTracker()
	ctx := t.Context()

	if err := tracker.Handle(ctx, client.Event{Type: "session.updated"}, "ses_1", nil); err != nil {
		t.Fatalf("unrelated event returned error: %v", err)
	}
	if err := tracker.Handle(ctx, client.Event{Type: "message.part.updated", Properties: json.RawMessage(`{`)}, "ses_1", nil); err == nil || !strings.Contains(err.Error(), "decode delegation tool event") {
		t.Fatalf("malformed event error=%v", err)
	}

	foreignProperties, err := json.Marshal(map[string]any{
		"sessionID": "ses_other",
		"part":      delegationToolPartPayload("ses_other", "part_foreign", "agent-2", "task"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Handle(ctx, client.Event{Type: "message.part.updated", Properties: foreignProperties}, "ses_1", nil); err != nil {
		t.Fatalf("foreign session event returned error: %v", err)
	}

	if _, err := decodeDelegationToolPart(json.RawMessage(`{`)); err == nil || !strings.Contains(err.Error(), "decode delegation tool part") {
		t.Fatalf("malformed part error=%v", err)
	}

	missingID, err := decodeDelegationToolPart(mustJSON(t, delegationToolPartPayload("ses_1", " ", "agent-2", "task")))
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.applyPart(ctx, missingID, "ses_1", &recordingDelegationRequester{}); err == nil || !strings.Contains(err.Error(), "missing part id") {
		t.Fatalf("missing id error=%v", err)
	}

	missingTarget, err := decodeDelegationToolPart(mustJSON(t, delegationToolPartPayload("ses_1", "part_target", " ", "task")))
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.applyPart(ctx, missingTarget, "ses_1", &recordingDelegationRequester{}); err == nil || !strings.Contains(err.Error(), "requires targetAgentId") {
		t.Fatalf("missing target error=%v", err)
	}

	missingTask, err := decodeDelegationToolPart(mustJSON(t, delegationToolPartPayload("ses_1", "part_task", "agent-2", " ")))
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.applyPart(ctx, missingTask, "ses_1", &recordingDelegationRequester{}); err == nil || !strings.Contains(err.Error(), "requires task") {
		t.Fatalf("missing task error=%v", err)
	}

	withoutCapability, err := decodeDelegationToolPart(mustJSON(t, delegationToolPartPayload("ses_1", "part_capability", "agent-2", "task")))
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.applyPart(ctx, withoutCapability, "ses_1", nil); err == nil || !strings.Contains(err.Error(), "capability is unavailable") {
		t.Fatalf("missing capability error=%v", err)
	}
}

func TestDelegationToolTrackerReconcileRequiresNativeClient(t *testing.T) {
	tracker := newDelegationToolTracker()
	if err := tracker.Reconcile(t.Context(), nil, "ses_1", &recordingDelegationRequester{}); err == nil || !strings.Contains(err.Error(), "native client is required") {
		t.Fatalf("nil native client error=%v", err)
	}
}
