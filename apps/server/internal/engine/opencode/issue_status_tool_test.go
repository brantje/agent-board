package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

type recordingIssueStatusUpdater struct {
	statuses  []string
	recovered []string
	err       error
}

func (u *recordingIssueStatusUpdater) SetStatus(_ context.Context, status string) error {
	u.statuses = append(u.statuses, status)
	return u.err
}

func (u *recordingIssueStatusUpdater) SetRecoveredStatus(ctx context.Context, status string) error {
	u.recovered = append(u.recovered, status)
	return u.SetStatus(ctx, status)
}

func TestIssueStatusServeCommandInstallsToolOutsideWorkspace(t *testing.T) {
	command := issueStatusServeCommand("127.0.0.1", "4100", nil)
	if len(command) != 7 || command[0] != "sh" || command[1] != "-c" {
		t.Fatalf("command=%q", command)
	}
	if command[4] != "127.0.0.1" || command[5] != "4100" {
		t.Fatalf("command host/port=%q", command)
	}
	if !strings.Contains(command[2], "XDG_CONFIG_HOME") || strings.Contains(command[2], "/workspace/.opencode") {
		t.Fatalf("tool installation script=%q", command[2])
	}
	toolSource := command[6]
	if !strings.Contains(toolSource, "@opencode-ai/plugin") || !strings.Contains(toolSource, "Board status") {
		t.Fatalf("tool source=%q", toolSource)
	}
}

func TestIssueStatusToolCompletionUsesRunScopedCapabilityOnce(t *testing.T) {
	tracker := newIssueStatusToolTracker()
	updater := &recordingIssueStatusUpdater{}
	event := issueStatusToolEvent(t, "ses_1", "part_1", "REVIEW")

	if err := tracker.Handle(context.Background(), event, "ses_1", updater); err != nil {
		t.Fatalf("Handle() error=%v", err)
	}
	if err := tracker.Handle(context.Background(), event, "ses_1", updater); err != nil {
		t.Fatalf("duplicate Handle() error=%v", err)
	}
	if len(updater.statuses) != 1 || updater.statuses[0] != "REVIEW" {
		t.Fatalf("statuses=%v", updater.statuses)
	}
	if len(updater.recovered) != 0 {
		t.Fatalf("live completion unexpectedly used recovery path: %v", updater.recovered)
	}

	other := issueStatusToolEvent(t, "ses_other", "part_2", "DONE")
	if err := tracker.Handle(context.Background(), other, "ses_1", updater); err != nil {
		t.Fatalf("other session Handle() error=%v", err)
	}
	if len(updater.statuses) != 1 {
		t.Fatalf("other session mutated statuses=%v", updater.statuses)
	}
}

func TestIssueStatusToolReconcileRecoversMissedCompletionOnce(t *testing.T) {
	native := issueStatusHistoryClient(t, "ses_1", []any{
		map[string]any{
			"info": map[string]any{"sessionID": "ses_1"},
			"parts": []any{issueStatusToolPartPayload("ses_1", "part_recovered", "BLOCKED")},
		},
	})
	tracker := newIssueStatusToolTracker()
	updater := &recordingIssueStatusUpdater{}

	if err := tracker.Reconcile(context.Background(), native, "ses_1", updater); err != nil {
		t.Fatalf("Reconcile() error=%v", err)
	}
	if err := tracker.Reconcile(context.Background(), native, "ses_1", updater); err != nil {
		t.Fatalf("duplicate Reconcile() error=%v", err)
	}
	if len(updater.statuses) != 1 || updater.statuses[0] != "BLOCKED" {
		t.Fatalf("statuses=%v", updater.statuses)
	}
	if len(updater.recovered) != 0 {
		t.Fatalf("in-process reconciliation unexpectedly used attach recovery path: %v", updater.recovered)
	}
}

func TestIssueStatusToolReconcileIgnoresOtherSession(t *testing.T) {
	native := issueStatusHistoryClient(t, "ses_1", []any{
		map[string]any{
			"info": map[string]any{"sessionID": "ses_other"},
			"parts": []any{issueStatusToolPartPayload("ses_other", "part_other", "DONE")},
		},
		map[string]any{
			"info": map[string]any{"sessionID": "ses_1"},
			"parts": []any{issueStatusToolPartPayload("ses_other", "part_mismatched", "REVIEW")},
		},
	})
	updater := &recordingIssueStatusUpdater{}
	if err := newIssueStatusToolTracker().Reconcile(context.Background(), native, "ses_1", updater); err != nil {
		t.Fatalf("Reconcile() error=%v", err)
	}
	if len(updater.statuses) != 0 {
		t.Fatalf("other session mutated statuses=%v", updater.statuses)
	}
}

func TestIssueStatusToolReconcileAttachRestoresLatestDurableIntentOnce(t *testing.T) {
	native := issueStatusHistoryClient(t, "ses_1", []any{
		map[string]any{
			"info": map[string]any{"sessionID": "ses_1"},
			"parts": []any{
				issueStatusToolPartPayload("ses_1", "part_started ", "IN_PROGRESS"),
				issueStatusToolPartPayload("ses_1", "part_review", "REVIEW"),
			},
		},
	})
	tracker := newIssueStatusToolTracker()
	updater := &recordingIssueStatusUpdater{}

	if err := tracker.ReconcileAttach(context.Background(), native, "ses_1", updater); err != nil {
		t.Fatalf("ReconcileAttach() error=%v", err)
	}
	if err := tracker.ReconcileAttach(context.Background(), native, "ses_1", updater); err != nil {
		t.Fatalf("duplicate ReconcileAttach() error=%v", err)
	}
	if err := tracker.Reconcile(context.Background(), native, "ses_1", updater); err != nil {
		t.Fatalf("Reconcile() after attach error=%v", err)
	}
	if len(updater.statuses) != 1 || updater.statuses[0] != "REVIEW" {
		t.Fatalf("statuses=%v want [REVIEW]", updater.statuses)
	}
	if len(updater.recovered) != 1 || updater.recovered[0] != "REVIEW" {
		t.Fatalf("recovered statuses=%v want [REVIEW]", updater.recovered)
	}
}

func TestIssueStatusToolCompletionValidatesAndPropagatesFailure(t *testing.T) {
	tracker := newIssueStatusToolTracker()
	missingStatus := issueStatusToolEvent(t, "ses_1", "part_1", "")
	if err := tracker.Handle(context.Background(), missingStatus, "ses_1", &recordingIssueStatusUpdater{}); err == nil {
		t.Fatal("missing status unexpectedly accepted")
	}

	updater := &recordingIssueStatusUpdater{err: errors.New("rejected")}
	if err := tracker.Handle(context.Background(), issueStatusToolEvent(t, "ses_1", "part_2", "DONE"), "ses_1", updater); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("failure=%v", err)
	}
}

func TestInitialTaskPromptExplainsExplicitBoardStatusSemantics(t *testing.T) {
	prompt := initialTaskPrompt(executioncontext.SafeContext{
		Issue: executioncontext.IssueContext{Title: "Implement explicit Issue status"},
	})
	for _, want := range []string{
		"set_issue_status(status)",
		"IN_PROGRESS",
		"BLOCKED",
		"REVIEW",
		"DONE",
		"Do not infer Board status from the Run lifecycle",
		"native Question capability",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func issueStatusHistoryClient(t *testing.T, sessionID string, messages []any) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session/"+sessionID+"/message", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, messages)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return native
}

func issueStatusToolEvent(t *testing.T, sessionID, partID, status string) client.Event {
	t.Helper()
	properties, err := json.Marshal(map[string]any{
		"sessionID": sessionID,
		"part":      issueStatusToolPartPayload(sessionID, partID, status),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client.Event{Type: "message.part.updated", Properties: properties}
}

func issueStatusToolPartPayload(sessionID, partID, status string) map[string]any {
	input := map[string]any{}
	if status != "" {
		input["status"] = status
	}
	return map[string]any{
		"id":        partID,
		"sessionID": sessionID,
		"type":      "tool",
		"tool":      issueStatusToolName,
		"state": map[string]any{
			"status": "completed",
			"input":  input,
		},
	}
}
