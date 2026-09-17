package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

type recordingDelegationRequester struct {
	requests []engine.DelegationRequest
	err      error
}

func (r *recordingDelegationRequester) Delegate(_ context.Context, request engine.DelegationRequest) (engine.Delegation, error) {
	r.requests = append(r.requests, request)
	if r.err != nil {
		return engine.Delegation{}, r.err
	}
	return engine.Delegation{ID: "delegation-1", RunID: "run-2"}, nil
}

type permissionReply struct {
	requestID string
	response  string
}

func TestOpenCodeServeCommandInstallsOnlyAvailableTools(t *testing.T) {
	tests := []struct {
		name       string
		status     bool
		delegation bool
		wantStatus bool
		wantDel    bool
	}{
		{name: "none"},
		{name: "status", status: true, wantStatus: true},
		{name: "delegation", delegation: true, wantDel: true},
		{name: "both", status: true, delegation: true, wantStatus: true, wantDel: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := openCodeServeCommand("127.0.0.1", "4100", tt.status, tt.delegation)
			if len(command) != 8 || command[0] != "sh" || command[1] != "-c" {
				t.Fatalf("command=%q", command)
			}
			if got := command[6] != ""; got != tt.wantStatus {
				t.Fatalf("status source present=%v want=%v", got, tt.wantStatus)
			}
			if got := command[7] != ""; got != tt.wantDel {
				t.Fatalf("delegation source present=%v want=%v", got, tt.wantDel)
			}
			if !strings.Contains(command[2], "rm -f") {
				t.Fatalf("tool install script does not remove stale tools: %q", command[2])
			}
		})
	}
	if !strings.Contains(delegationToolSource, "context.ask") || !strings.Contains(delegationToolSource, delegationPermissionName) || !strings.Contains(delegationToolSource, "context.callID") {
		t.Fatalf("delegation tool source does not synchronously bind to Agent Board: %s", delegationToolSource)
	}
}

func TestDelegationPermissionSuccessCompletesOnlyAfterCanonicalRequest(t *testing.T) {
	var replies []permissionReply
	native := delegationPermissionClient(t, nil, &replies)
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}
	event := delegationPermissionEvent(t, delegationPermissionRequest(t, "per_1", "ses_1", "call_1", "agent-2", "inspect scheduler ownership"))

	if err := tracker.Handle(t.Context(), event, native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("requests=%v", requester.requests)
	}
	got := requester.requests[0]
	if got.TargetAgentID != "agent-2" || got.Task != "inspect scheduler ownership" || got.RequestKey != "call_1" {
		t.Fatalf("request=%+v", got)
	}
	if len(replies) != 1 || replies[0].requestID != "per_1" || replies[0].response != "once" {
		t.Fatalf("permission replies=%+v", replies)
	}
}

func TestDelegationPermissionRejectionSurfacesAsToolFailure(t *testing.T) {
	var replies []permissionReply
	native := delegationPermissionClient(t, nil, &replies)
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{err: errors.New("self delegation is not allowed")}
	event := delegationPermissionEvent(t, delegationPermissionRequest(t, "per_1", "ses_1", "call_1", "agent-2", "task"))

	if err := tracker.Handle(t.Context(), event, native, "ses_1", requester); err != nil {
		t.Fatalf("backend rejection should be returned to OpenCode via permission rejection, got %v", err)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("requests=%v", requester.requests)
	}
	if len(replies) != 1 || replies[0].response != "reject" {
		t.Fatalf("permission replies=%+v", replies)
	}
}

func TestDelegationPermissionReplayIsIdempotent(t *testing.T) {
	pending := []client.PermissionRequest{
		delegationPermissionRequest(t, "per_1", "ses_1", "call_1", "agent-2", "task"),
	}
	var replies []permissionReply
	native := delegationPermissionClient(t, pending, &replies)
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}

	if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 1 || requester.requests[0].RequestKey != "call_1" {
		t.Fatalf("requests=%+v", requester.requests)
	}
	if len(replies) != 2 || replies[0].response != "once" || replies[1].response != "once" {
		t.Fatalf("permission replies=%+v", replies)
	}
}

func TestDelegationCompletedToolEventDoesNotCreateAnotherRequest(t *testing.T) {
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}
	event := client.Event{Type: "message.part.updated", Properties: mustJSON(t, map[string]any{
		"sessionID": "ses_1",
		"part": map[string]any{
			"id": "part_1", "sessionID": "ses_1", "type": "tool", "tool": delegationToolName,
			"state": map[string]any{"status": "completed", "input": map[string]any{"targetAgentId": "agent-2", "task": "task"}},
		},
	})}
	if err := tracker.Handle(t.Context(), event, nil, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 0 {
		t.Fatalf("completed tool replay created delegation requests: %+v", requester.requests)
	}
}

func TestInitialTaskPromptScopesDelegatedExecution(t *testing.T) {
	prompt := initialTaskPrompt(executioncontext.SafeContext{
		Issue: executioncontext.IssueContext{Title: "Parent issue", Status: "IN_PROGRESS"},
		Agent: executioncontext.AgentContext{AllowDelegation: true},
		Delegation: &executioncontext.DelegationContext{
			Task: "Only inspect the queue handoff",
		},
	})
	for _, want := range []string{"Delegated task:", "Only inspect the queue handoff", "parent Run remains authoritative"} {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(want)) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "set_issue_status(status)") || strings.Contains(prompt, "delegate_task(targetAgentId, task)") {
		t.Fatalf("delegated prompt exposes authoritative tool guidance: %s", prompt)
	}
}

func TestInitialTaskPromptAdvertisesDelegationForAuthorizedParent(t *testing.T) {
	prompt := initialTaskPrompt(executioncontext.SafeContext{
		Issue: executioncontext.IssueContext{Title: "Parent issue", Status: "IN_PROGRESS"},
		Agent: executioncontext.AgentContext{AllowDelegation: true},
	})
	if !strings.Contains(prompt, "delegate_task(targetAgentId, task)") {
		t.Fatalf("prompt=%s", prompt)
	}
}

func delegationPermissionClient(t *testing.T, pending []client.PermissionRequest, replies *[]permissionReply) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /permission", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pending); err != nil {
			t.Fatal(err)
		}
	})
	mux.HandleFunc("POST /permission/{requestID}/reply", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Reply string `json:"reply"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		*replies = append(*replies, permissionReply{requestID: r.PathValue("requestID"), response: payload.Reply})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("true"))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return native
}

func delegationPermissionEvent(t *testing.T, permission client.PermissionRequest) client.Event {
	t.Helper()
	return client.Event{Type: "permission.asked", Properties: mustJSON(t, permission)}
}

func delegationPermissionRequest(t *testing.T, requestID, sessionID, callID, targetAgentID, task string) client.PermissionRequest {
	t.Helper()
	metadata := mustJSON(t, delegationPermissionMetadata{
		Tool: delegationToolName, TargetAgentID: targetAgentID, Task: task, CallID: callID,
	})
	request := client.PermissionRequest{
		ID: requestID, SessionID: sessionID, Permission: delegationPermissionName,
		Patterns: []string{callID}, Metadata: metadata,
	}
	request.Tool = &struct {
		MessageID string `json:"messageID"`
		CallID    string `json:"callID"`
	}{MessageID: "msg_1", CallID: callID}
	return request
}
