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


func TestDelegationHandoffNilTrackerFailsClosed(t *testing.T) {
	var tracker *delegationToolTracker
	if err := tracker.Handle(t.Context(), client.Event{}, nil, "ses_1", nil); err == nil {
		t.Fatal("nil delegation tracker Handle did not fail closed")
	}
	if err := tracker.Reconcile(t.Context(), nil, "ses_1", nil); err == nil {
		t.Fatal("nil delegation tracker Reconcile did not fail closed")
	}
}

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

func TestDelegationHandoffAcceptedNativeErrorConvergesWithoutSecondRequest(t *testing.T) {
	var replies []permissionReply
	native := delegationPermissionClient(t, nil, &replies)
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}
	permission := delegationPermissionEvent(t, delegationPermissionRequest(t, "per_1", "ses_1", "call_1", "agent-2", "inspect scheduler ownership"))
	if err := tracker.Handle(t.Context(), permission, native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("canonical requests after permission=%d want 1", len(requester.requests))
	}

	errored := terminalDelegationToolEvent(t, "ses_1", "part_1", "call_1", "agent-2", "inspect scheduler ownership", "error")
	if err := tracker.Handle(t.Context(), errored, native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("accepted native error replayed canonical request: %d", len(requester.requests))
	}
	if tracker.handoff == nil || tracker.handoff.ID != "delegation-1" || tracker.handoff.RunID != "run-2" {
		t.Fatalf("accepted native error did not arm existing handoff: %+v", tracker.handoff)
	}
}

func TestDelegationHandoffRecoveryUsesDurableAcceptedNativeError(t *testing.T) {
	part := map[string]any{
		"id": "part_1", "callID": "call_1", "sessionID": "ses_1", "type": "tool", "tool": delegationToolName,
		"state": map[string]any{
			"status": "error",
			"input":  map[string]any{"targetAgentId": "agent-2", "task": "recover handoff"},
		},
	}
	native := delegationRecoveryClient(t, []any{part})
	tracker := newDelegationToolTracker()
	request := engine.DelegationRequest{TargetAgentID: "agent-2", Task: "recover handoff", RequestKey: "call_1"}
	requester := &recordingDelegationRequester{accepted: map[string]acceptedDelegation{
		"call_1": {request: request, delegation: engine.Delegation{ID: "delegation-1", RunID: "run-2"}},
	}}

	err := tracker.Reconcile(t.Context(), native, "ses_1", requester)
	if !errors.Is(err, engine.ErrDelegationHandoff) {
		t.Fatalf("recovery err=%v want handoff", err)
	}
	if len(requester.requests) != 0 {
		t.Fatalf("errored history invoked Delegate directly: %+v", requester.requests)
	}
	if len(requester.resolved) != 1 || !sameDelegationRequest(requester.resolved[0], request) {
		t.Fatalf("durable acceptance resolutions=%+v", requester.resolved)
	}
}

func TestDelegationHandoffRecoveryKeepsRejectedNativeErrorRejected(t *testing.T) {
	part := map[string]any{
		"id": "part_1", "callID": "call_1", "sessionID": "ses_1", "type": "tool", "tool": delegationToolName,
		"state": map[string]any{
			"status": "error",
			"input":  map[string]any{"targetAgentId": "agent-2", "task": "rejected handoff"},
		},
	}
	native := delegationRecoveryClient(t, []any{part})
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}

	if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err != nil {
		t.Fatalf("rejected error history should remain non-authoritative: %v", err)
	}
	if len(requester.requests) != 0 || len(requester.resolved) != 1 || tracker.handoff != nil {
		t.Fatalf("rejected history created authority: requests=%+v resolved=%+v handoff=%+v", requester.requests, requester.resolved, tracker.handoff)
	}
}

func TestDelegationHandoffRecoveryFailsClosedOnAcceptedNativeErrorIdentityMismatch(t *testing.T) {
	part := map[string]any{
		"id": "part_1", "callID": "call_1", "sessionID": "ses_1", "type": "tool", "tool": delegationToolName,
		"state": map[string]any{
			"status": "error",
			"input":  map[string]any{"targetAgentId": "agent-2", "task": "native task"},
		},
	}
	native := delegationRecoveryClient(t, []any{part})
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{accepted: map[string]acceptedDelegation{
		"call_1": {
			request: engine.DelegationRequest{TargetAgentID: "agent-2", Task: "different task", RequestKey: "call_1"},
			delegation: engine.Delegation{ID: "delegation-1", RunID: "run-2"},
		},
	}}

	if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err == nil {
		t.Fatal("mismatched accepted native error was not rejected")
	}
	if len(requester.requests) != 0 || tracker.handoff != nil {
		t.Fatalf("mismatched error created handoff: requests=%+v handoff=%+v", requester.requests, tracker.handoff)
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
	native := delegationRecoveryClient(t, []any{part})
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}

	err := tracker.Reconcile(t.Context(), native, "ses_1", requester)
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

func TestDelegationHandoffRecoveryFailsClosedOnInvalidCompletedHistory(t *testing.T) {
	validInput := map[string]any{"targetAgentId": "agent-2", "task": "recover handoff"}
	tests := []struct {
		name string
		part any
	}{
		{name: "malformed part", part: "not-a-tool-part"},
		{name: "missing call id", part: map[string]any{
			"id": "part_1", "sessionID": "ses_1", "type": "tool", "tool": delegationToolName,
			"state": map[string]any{"status": "completed", "input": validInput},
		}},
		{name: "invalid input", part: map[string]any{
			"id": "part_1", "callID": "call_1", "sessionID": "ses_1", "type": "tool", "tool": delegationToolName,
			"state": map[string]any{"status": "completed", "input": map[string]any{"targetAgentId": "agent-2"}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			native := delegationRecoveryClient(t, []any{tc.part})
			tracker := newDelegationToolTracker()
			requester := &recordingDelegationRequester{}

			if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err == nil {
				t.Fatal("invalid durable delegation history was accepted")
			}
			if len(requester.requests) != 0 || tracker.handoff != nil {
				t.Fatalf("invalid recovery created delegation: requests=%+v handoff=%+v", requester.requests, tracker.handoff)
			}
		})
	}
}

func TestDelegationHandoffRecoveryFailsClosedOnUnavailableOrRejectedAuthority(t *testing.T) {
	part := map[string]any{
		"id": "part_1", "callID": "call_1", "sessionID": "ses_1", "type": "tool", "tool": delegationToolName,
		"state": map[string]any{
			"status": "completed",
			"input":  map[string]any{"targetAgentId": "agent-2", "task": "recover handoff"},
		},
	}

	t.Run("native history unavailable", func(t *testing.T) {
		tracker := newDelegationToolTracker()
		requester := &recordingDelegationRequester{}
		if err := tracker.Reconcile(t.Context(), nil, "ses_1", requester); err == nil {
			t.Fatal("missing native history was accepted")
		}
	})

	t.Run("canonical replay rejected", func(t *testing.T) {
		native := delegationRecoveryClient(t, []any{part})
		tracker := newDelegationToolTracker()
		requester := &recordingDelegationRequester{err: errors.New("delegation no longer allowed")}
		if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err == nil {
			t.Fatal("rejected canonical delegation replay was accepted")
		}
		if len(requester.requests) != 1 || tracker.handoff != nil {
			t.Fatalf("rejected replay state: requests=%+v handoff=%+v", requester.requests, tracker.handoff)
		}
	})

	t.Run("accepted input changed", func(t *testing.T) {
		native := delegationRecoveryClient(t, []any{part})
		tracker := newDelegationToolTracker()
		tracker.accepted["call_1"] = acceptedDelegation{
			request: engine.DelegationRequest{TargetAgentID: "agent-2", Task: "different task", RequestKey: "call_1"},
			delegation: engine.Delegation{ID: "delegation-1", RunID: "run-2"},
		}
		requester := &recordingDelegationRequester{}
		if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err == nil {
			t.Fatal("changed recovered delegation input was accepted")
		}
		if len(requester.requests) != 0 || tracker.handoff != nil {
			t.Fatalf("changed recovery state: requests=%+v handoff=%+v", requester.requests, tracker.handoff)
		}
	})
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
	return terminalDelegationToolEvent(t, sessionID, partID, callID, targetAgentID, task, "completed")
}

func terminalDelegationToolEvent(t *testing.T, sessionID, partID, callID, targetAgentID, task, status string) client.Event {
	t.Helper()
	return client.Event{Type: "message.part.updated", Properties: mustJSON(t, map[string]any{
		"sessionID": sessionID,
		"part": map[string]any{
			"id": partID, "callID": callID, "sessionID": sessionID, "type": "tool", "tool": delegationToolName,
			"state": map[string]any{
				"status": status,
				"input":  map[string]any{"targetAgentId": targetAgentID, "task": task},
			},
		},
	})}
}

func delegationRecoveryClient(t *testing.T, parts []any) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /permission", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("GET /session/ses_1/message", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"info":  map[string]any{"sessionID": "ses_1"},
			"parts": parts,
		}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return native
}
