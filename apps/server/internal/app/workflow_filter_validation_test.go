package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestQuestionServiceListValidatesStatusDomain(t *testing.T) {
	questionStore := &questionServiceStore{}
	service, err := NewQuestionService(questionStore)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.List(context.Background(), "project-1", store.QuestionFilter{Statuses: []string{"OPEN", "INVALID"}})
	if appErr, ok := AsError(err); !ok || appErr.Code != "invalid_argument" || appErr.Message != "status must be OPEN, ANSWERED or CANCELLED" {
		t.Fatalf("invalid Question status error=%v", err)
	}
	if len(questionStore.lastFilter.Statuses) != 0 {
		t.Fatalf("invalid Question filter reached store: %+v", questionStore.lastFilter)
	}

	want := []string{"OPEN", "ANSWERED", "CANCELLED"}
	if _, err := service.List(context.Background(), "project-1", store.QuestionFilter{Statuses: want}); err != nil {
		t.Fatalf("valid Question statuses rejected: %v", err)
	}
	if len(questionStore.lastFilter.Statuses) != len(want) {
		t.Fatalf("valid Question filter=%+v", questionStore.lastFilter)
	}
}

func TestReviewServiceListValidatesStatusDomain(t *testing.T) {
	reviewStore := &reviewServiceStore{list: []store.Review{{
		ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: "run-1", Status: "PENDING",
		BaseRevision: "base", ReviewRevision: "review",
	}}}
	service := newReviewServiceForTest(t, reviewStore, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})

	_, err := service.List(context.Background(), "project-1", store.ReviewFilter{Statuses: []string{"PENDING", "INVALID"}})
	if appErr, ok := AsError(err); !ok || appErr.Code != "invalid_argument" || appErr.Message != "status must be PENDING, APPROVED, CHANGES_REQUESTED or CANCELLED" {
		t.Fatalf("invalid Review status error=%v", err)
	}

	want := []string{"PENDING", "APPROVED", "CHANGES_REQUESTED", "CANCELLED"}
	values, err := service.List(context.Background(), "project-1", store.ReviewFilter{Statuses: want})
	if err != nil || len(values) != 1 {
		t.Fatalf("valid Review statuses values=%+v err=%v", values, err)
	}
}
