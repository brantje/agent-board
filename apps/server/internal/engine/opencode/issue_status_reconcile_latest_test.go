package opencode

import (
	"context"
	"testing"
)

func TestIssueStatusToolReconcileUsesLatestDurableIntent(t *testing.T) {
	native := issueStatusHistoryClient(t, "ses_1", []any{
		map[string]any{
			"info": map[string]any{"sessionID": "ses_1"},
			"parts": []any{
				issueStatusToolPartPayload("ses_1", "part_in_progress", "IN_PROGRESS"),
				issueStatusToolPartPayload("ses_1", "part_review", "REVIEW"),
			},
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
	if len(updater.recovered) != 1 || updater.recovered[0] != "REVIEW" {
		t.Fatalf("recovered statuses=%v want [REVIEW]", updater.recovered)
	}
	if len(updater.statuses) != 1 || updater.statuses[0] != "REVIEW" {
		t.Fatalf("applied statuses=%v want [REVIEW]", updater.statuses)
	}
	if _, ok := tracker.seen["part_in_progress"]; !ok {
		t.Fatal("older durable status intent was not marked seen")
	}
	if _, ok := tracker.seen["part_review"]; !ok {
		t.Fatal("latest durable status intent was not marked seen")
	}
}
