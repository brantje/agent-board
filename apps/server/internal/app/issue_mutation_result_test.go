package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type atomicIssueMutationStore struct {
	fakeStore
	create store.IssueMutationResult
	update store.IssueMutationResult
	err    error
}

func (s *atomicIssueMutationStore) CreateIssueMutation(context.Context, store.Issue) (store.IssueMutationResult, error) {
	return s.create, s.err
}

func (s *atomicIssueMutationStore) UpdateIssueMutation(context.Context, store.Issue) (store.IssueMutationResult, error) {
	return s.update, s.err
}

func TestIssueMutationStorePublishesCommittedEventsWithoutRecordingAgain(t *testing.T) {
	const projectID = "project"
	created := store.Event{ID: "issue-created", Type: "issue.created", ProjectID: projectID}
	statusChanged := store.Event{ID: "status-changed", Type: "issue.status_changed", ProjectID: projectID}
	runCreated := store.Event{ID: "run-created", Type: "run.created", ProjectID: projectID}
	createdIssue := store.Issue{ID: "issue", ProjectID: projectID, Title: "Issue", Status: "TODO", LastEvent: &created}
	updatedIssue := createdIssue
	updatedIssue.Status = "IN_PROGRESS"
	updatedIssue.LastEvent = &statusChanged

	fake := &atomicIssueMutationStore{
		fakeStore: fakeStore{project: store.Project{ID: projectID}},
		create:    store.IssueMutationResult{Issue: createdIssue, Events: []store.Event{runCreated, created}},
		update:    store.IssueMutationResult{Issue: updatedIssue, Events: []store.Event{statusChanged}},
	}
	svc := New(fake)
	publisher := &assigneePublisher{}
	svc.SetEventRecorder(publisher)

	got, err := svc.CreateIssue(t.Context(), store.Issue{ProjectID: projectID, Title: "Issue", Status: "TODO"})
	if err != nil || got.LastEvent == nil || got.LastEvent.ID != created.ID {
		t.Fatalf("CreateIssue() issue=%+v err=%v", got, err)
	}
	if len(publisher.published) != 2 || publisher.published[0].ID != runCreated.ID || publisher.published[1].ID != created.ID {
		t.Fatalf("create published=%+v", publisher.published)
	}

	got, err = svc.UpdateIssue(t.Context(), store.Issue{ID: "issue", ProjectID: projectID, Title: "Issue", Status: "IN_PROGRESS"})
	if err != nil || got.LastEvent == nil || got.LastEvent.ID != statusChanged.ID {
		t.Fatalf("UpdateIssue() issue=%+v err=%v", got, err)
	}
	if len(publisher.published) != 3 || publisher.published[2].ID != statusChanged.ID {
		t.Fatalf("update published=%+v", publisher.published)
	}
}
