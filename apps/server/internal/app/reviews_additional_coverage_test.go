package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestReviewServiceListAndDecisionProjection(t *testing.T) {
	run := store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"}
	decisionID := "decision-1"
	review := store.Review{
		ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: run.ID, Status: "APPROVED", DecisionID: &decisionID,
		BaseRevision: "base-revision", ReviewRevision: "review-revision",
	}
	s := &reviewServiceStore{
		run:      run,
		review:   review,
		list:     []store.Review{review},
		decision: store.Decision{ID: decisionID, Kind: "REVIEW", Outcome: "APPROVED"},
	}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	values, err := service.List(context.Background(), "project-1", store.ReviewFilter{Statuses: []string{"APPROVED"}})
	if err != nil || len(values) != 1 || values[0].ID != review.ID || values[0].ReviewRevision != "review-revision" {
		t.Fatalf("List()=%+v err=%v", values, err)
	}
	inspection, err := service.Get(context.Background(), "project-1", review.ID)
	if err != nil || inspection.Decision == nil || inspection.Decision.ID != decisionID {
		t.Fatalf("Get()=%+v err=%v", inspection, err)
	}

	s.decision.Kind = "REVIEW_APPROVAL_INTENT"
	inspection, err = service.Get(context.Background(), "project-1", review.ID)
	if err != nil || inspection.Decision != nil {
		t.Fatalf("lifecycle decision leaked: %+v err=%v", inspection, err)
	}
}

func TestReviewServiceValidatesCommandIdentifiers(t *testing.T) {
	service := newReviewServiceForTest(t, &reviewServiceStore{}, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	if _, err := service.List(context.Background(), " ", store.ReviewFilter{}); err == nil {
		t.Fatal("List should reject blank project")
	}
	if _, err := service.Get(context.Background(), "", "review"); err == nil {
		t.Fatal("Get should reject blank project")
	}
	if _, err := service.Get(context.Background(), "project", " "); err == nil {
		t.Fatal("Get should reject blank review")
	}
	if _, err := service.Approve(context.Background(), "", "review", nil); err == nil {
		t.Fatal("Approve should reject blank project")
	}
	if _, err := service.Approve(context.Background(), "project", "", nil); err == nil {
		t.Fatal("Approve should reject blank review")
	}
	if _, err := service.RequestChanges(context.Background(), "", "review", "fix", nil); err == nil {
		t.Fatal("RequestChanges should reject blank project")
	}
	if _, err := service.RequestChanges(context.Background(), "project", "", "fix", nil); err == nil {
		t.Fatal("RequestChanges should reject blank review")
	}
}

func TestReviewServiceApprovalFailuresRemainRecoverable(t *testing.T) {
	run := store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"}
	review := store.Review{
		ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: run.ID, Status: "PENDING",
		BaseRevision: "base-revision", ReviewRevision: "review-revision",
	}
	blobs := &reviewBlobStore{values: map[string][]byte{}}

	t.Run("begin persistence failure", func(t *testing.T) {
		s := &reviewServiceStore{run: run, review: review, beginErr: store.ErrConflict}
		service := newReviewServiceForTest(t, s, blobs, &reviewCandidateApplierFake{})
		if _, err := service.Approve(context.Background(), "project-1", review.ID, nil); err == nil {
			t.Fatal("expected begin failure")
		}
	})

	t.Run("apply failure records retryable failure", func(t *testing.T) {
		s := &reviewServiceStore{
			project: store.Project{ID: "project-1", SourceType: store.ProjectSourceLocal},
			run:     run,
			review:  review,
			begin:   store.BeginReviewApprovalResult{Review: review, Run: run},
		}
		service := newReviewServiceForTest(t, s, blobs, &reviewCandidateApplierFake{err: errors.New("apply failed")})
		if _, err := service.Approve(context.Background(), "project-1", review.ID, nil); err == nil {
			t.Fatal("expected apply failure")
		}
		if s.failCalls != 1 {
			t.Fatalf("failure persistence calls=%d", s.failCalls)
		}
	})

	t.Run("post-apply completion failure leaves intent recoverable", func(t *testing.T) {
		s := &reviewServiceStore{
			project:     store.Project{ID: "project-1", SourceType: store.ProjectSourceLocal},
			run:         run,
			review:      review,
			begin:       store.BeginReviewApprovalResult{Review: review, Run: run},
			completeErr: store.ErrConflict,
		}
		service := newReviewServiceForTest(t, s, blobs, &reviewCandidateApplierFake{revision: "accepted"})
		if _, err := service.Approve(context.Background(), "project-1", review.ID, nil); err == nil {
			t.Fatal("expected completion failure")
		}
		if s.failCalls != 0 {
			t.Fatalf("post-apply failure must not fail approval intent: calls=%d", s.failCalls)
		}
	})
}

func TestReviewServiceRequestChangesTranslatesStoreFailure(t *testing.T) {
	s := &reviewServiceStore{requestErr: store.ErrConflict}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	if _, err := service.RequestChanges(context.Background(), "project", "review", "fix", nil); err == nil {
		t.Fatal("expected store failure")
	}
}
