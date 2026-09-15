package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewContinuationEventStore struct {
	*reviewServiceStore
}

func (s *reviewContinuationEventStore) RequestReviewChanges(_ context.Context, command store.RequestReviewChangesCommand) (store.RequestReviewChangesResult, error) {
	issueID, runID, agentID, workspaceID := "issue-1", "run-2", "agent-1", "workspace-1"
	return store.RequestReviewChangesResult{
		Review: store.Review{ID: command.ReviewID, ProjectID: command.ProjectID, IssueID: issueID, RunID: "run-1", Status: "CHANGES_REQUESTED"},
		Run:    store.Run{ID: runID, ProjectID: command.ProjectID, IssueID: issueID, WorkspaceID: workspaceID, AgentID: &agentID, Attempt: 2, Status: "QUEUED"},
		Events: []store.Event{{ID: "event-run-created", Type: "run.created", ProjectID: command.ProjectID, IssueID: &issueID, RunID: &runID, AgentID: &agentID, WorkspaceID: &workspaceID}},
	}, nil
}

func TestReviewRequestChangesPublishesContinuationCreatedEvent(t *testing.T) {
	publisher := &capturingPublisher{}
	service := &ReviewService{
		store:     &reviewContinuationEventStore{reviewServiceStore: &reviewServiceStore{}},
		publisher: publisher,
	}

	result, err := service.RequestChanges(t.Context(), "project-1", "review-1", "fix it", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.ID != "run-2" || len(result.Events) != 1 || result.Events[0].Type != "run.created" {
		t.Fatalf("result=%+v", result)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != "run.created" {
		t.Fatalf("published=%+v", publisher.events)
	}
}
