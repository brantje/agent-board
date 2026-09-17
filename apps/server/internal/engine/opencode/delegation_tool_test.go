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
}

func TestDelegationToolCompletionUsesPartIDAsRequestKeyOnce(t *testing.T) {
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}
	event := delegationToolEvent(t, "ses_1", "part_1", "agent-2", "inspect scheduler ownership")

	if err := tracker.Handle(t.Context(), event, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Handle(t.Context(), event, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("requests=%v", requester.requests)
	}
	got := requester.requests[0]
	if got.TargetAgentID != "agent-2" || got.Task != "inspect scheduler ownership" || got.RequestKey != "part_1" {
		t.Fatalf("request=%+v", got)
	}
}

func TestDelegationToolReconcileReplaysEveryDurableCompletion(t *testing.T) {
	native := delegationHistoryClient(t, "ses_1", []any{
		map[string]any{
			"info": map[string]any{"sessionID": "ses_1"},
			"parts": []any{
				delegationToolPartPayload("ses_1", "part_1", "agent-2", "task one"),
				delegationToolPartPayload("ses_1", "part_2", "agent-3", "task two"),
			},
		},
	})
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{}

	if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Reconcile(t.Context(), native, "ses_1", requester); err != nil {
		t.Fatal(err)
	}
	if len(requester.requests) != 2 {
		t.Fatalf("requests=%v", requester.requests)
	}
	if requester.requests[0].RequestKey != "part_1" || requester.requests[1].RequestKey != "part_2" {
		t.Fatalf("request keys=%v", requester.requests)
	}
}

func TestDelegationToolFailureIsRetried(t *testing.T) {
	tracker := newDelegationToolTracker()
	requester := &recordingDelegationRequester{err: errors.New("conflict")}
	event := delegationToolEvent(t, "ses_1", "part_1", "agent-2", "task")
	for attempt := 0; attempt < 2; attempt++ {
		err := tracker.Handle(t.Context(), event, "ses_1", requester)
		if err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("attempt %d err=%v", attempt+1, err)
		}
	}
	if len(requester.requests) != 2 {
		t.Fatalf("requests=%v", requester.requests)
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

func delegationHistoryClient(t *testing.T, sessionID string, messages []any) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session/"+sessionID+"/message", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(messages); err != nil {
			t.Fatal(err)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return native
}

func delegationToolEvent(t *testing.T, sessionID, partID, targetAgentID, task string) client.Event {
	t.Helper()
	properties, err := json.Marshal(map[string]any{
		"sessionID": sessionID,
		"part":      delegationToolPartPayload(sessionID, partID, targetAgentID, task),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client.Event{Type: "message.part.updated", Properties: properties}
}

func delegationToolPartPayload(sessionID, partID, targetAgentID, task string) map[string]any {
	return map[string]any{
		"id":        partID,
		"sessionID": sessionID,
		"type":      "tool",
		"tool":      delegationToolName,
		"state": map[string]any{
			"status": "completed",
			"input": map[string]any{
				"targetAgentId": targetAgentID,
				"task":          task,
			},
		},
	}
}
