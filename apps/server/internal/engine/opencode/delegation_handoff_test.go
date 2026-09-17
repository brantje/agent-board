package opencode

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestDelegationHandoffWaitsForCompletedToolPart(t *testing.T) {
	var replies []permissionReply
	native := delegationPermissionClient(t, nil, &replies)
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}
	permission := delegationPermissionEvent(t, delegationPermissionRequest(t, "per_1", "ses_1", "call_1", "agent-2", "inspect scheduler ownership"))
	if err := tracker.Handle(t.Context(), permission, native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if tracker.handoff != nil {
		t.Fatal("permission acceptance yielded before the native tool completed")
	}

	completed := completedDelegationToolEvent(t, "ses_1", "part_1", "call_1", "agent-2", "inspect scheduler ownership")
	if err := tracker.Handle(t.Context(), completed, native, "ses_1", requester); err != nil {
		t.Fatalf("completion must remain visible to normal activity handling: %v", err)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("canonical requests=%d want 1", len(requester.requests))
	}
	if tracker.handoff == nil {
		t.Fatal("completed accepted tool did not arm handoff")
	}

	err := tracker.Reconcile(t.Context(), nil, "ses_1", requester)
	if !errors.Is(err, engine.ErrDelegationHandoff) {
		t.Fatalf("handoff err=%v", err)
	}
	delegation, ok := engine.AsDelegationHandoff(err)
	if !ok || delegation.ID != "delegation-1" || delegation.RunID != "run-2" {
		t.Fatalf("handoff delegation=%+v ok=%v", delegation, ok)
	}
}

func TestDelegationHandoffRecoveryUsesDurableCompletedToolPart(t *testing.T) {
	part := map[string]any{
		"id": "part_1", "callID": "call_1", "sessionID": "ses_1", "type": "tool", "tool": delegationToolName,
		"state": map[string]any{
			"status": "completed",
			"input":  map[string]any{"targetAgentId": "agent-2", "task": "recover handoff"},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /permission", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("GET /session/ses_1/message", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"info":  map[string]any{"sessionID": "ses_1"},
			"parts": []any{part},
		}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}

	err = tracker.Reconcile(t.Context(), native, "ses_1", requester)
	if !errors.Is(err, engine.ErrDelegationHandoff) {
		t.Fatalf("recovery err=%v want handoff", err)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("recovered canonical requests=%+v", requester.requests)
	}
	got := requester.requests[0]
	if got.RequestKey != "call_1" || got.TargetAgentID != "agent-2" || got.Task != "recover handoff" {
		t.Fatalf("recovered request=%+v", got)
	}
}

func TestDelegationHandoffRejectsChangedCompletedInput(t *testing.T) {
	var replies []permissionReply
	native := delegationPermissionClient(t, nil, &replies)
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}
	permission := delegationPermissionEvent(t, delegationPermissionRequest(t, "per_1", "ses_1", "call_1", "agent-2", "original task"))
	if err := tracker.Handle(t.Context(), permission, native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	completed := completedDelegationToolEvent(t, "ses_1", "part_1", "call_1", "agent-2", "changed task")
	if err := tracker.Handle(t.Context(), completed, native, "ses_1", requester); err == nil {
		t.Fatal("changed completed tool input was accepted")
	}
	if tracker.handoff != nil {
		t.Fatal("changed completed input armed a handoff")
	}
}

func completedDelegationToolEvent(t *testing.T, sessionID, partID, callID, targetAgentID, task string) client.Event {
	t.Helper()
	return client.Event{Type: "message.part.updated", Properties: mustJSON(t, map[string]any{
		"sessionID": sessionID,
		"part": map[string]any{
			"id": partID, "callID": callID, "sessionID": sessionID, "type": "tool", "tool": delegationToolName,
			"state": map[string]any{
				"status": "completed",
				"input":  map[string]any{"targetAgentId": targetAgentID, "task": task},
			},
		},
	})}
}
