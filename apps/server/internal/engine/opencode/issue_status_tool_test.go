package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

type recordingIssueStatusUpdater struct {
	statuses []string
	err      error
}

func (u *recordingIssueStatusUpdater) SetStatus(_ context.Context, status string) error {
	u.statuses = append(u.statuses, status)
	return u.err
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

	other := issueStatusToolEvent(t, "ses_other", "part_2", "DONE")
	if err := tracker.Handle(context.Background(), other, "ses_1", updater); err != nil {
		t.Fatalf("other session Handle() error=%v", err)
	}
	if len(updater.statuses) != 1 {
		t.Fatalf("other session mutated statuses=%v", updater.statuses)
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

func issueStatusToolEvent(t *testing.T, sessionID, partID, status string) client.Event {
	t.Helper()
	input := map[string]any{}
	if status != "" {
		input["status"] = status
	}
	part := map[string]any{
		"id":        partID,
		"sessionID": sessionID,
		"type":      "tool",
		"tool":      issueStatusToolName,
		"state": map[string]any{
			"status": "completed",
			"input":  input,
		},
	}
	properties, err := json.Marshal(map[string]any{"sessionID": sessionID, "part": part})
	if err != nil {
		t.Fatal(err)
	}
	return client.Event{Type: "message.part.updated", Properties: properties}
}
