package app

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSetIssueAssigneeAcceptsSquadTarget(t *testing.T) {
	const (
		projectID = "00000000-0000-4000-8000-000000000001"
		squadID   = "00000000-0000-4000-8000-000000000004"
	)
	fake := &assigneeCommandStore{projectWorkflowAuthorizationStore: &projectWorkflowAuthorizationStore{}}
	service := New(fake)
	target := &store.Assignee{Type: "SQUAD", ID: squadID}

	if _, err := service.SetIssueAssignee(t.Context(), projectID, "issue", target, store.EmptyObject); err != nil {
		t.Fatalf("SetIssueAssignee: %v", err)
	}
	if fake.calls != 1 || len(fake.targets) != 1 || fake.targets[0] == nil || fake.targets[0].Type != "SQUAD" || fake.targets[0].ID != squadID {
		t.Fatalf("targets=%#v calls=%d", fake.targets, fake.calls)
	}
}
