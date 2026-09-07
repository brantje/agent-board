package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestReviewCommandsValidateRequiredInput(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()

	for name, command := range map[string]func() error{
		"begin blank project": func() error {
			_, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ReviewID: "review"})
			return err
		},
		"begin blank review": func() error {
			_, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: "project"})
			return err
		},
		"complete blank revision": func() error {
			_, err := s.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{ProjectID: "project", ReviewID: "review"})
			return err
		},
		"fail blank reason": func() error {
			_, err := s.FailReviewApproval(ctx, store.FailReviewApprovalCommand{ProjectID: "project", ReviewID: "review"})
			return err
		},
		"fail oversized reason": func() error {
			_, err := s.FailReviewApproval(ctx, store.FailReviewApprovalCommand{ProjectID: "project", ReviewID: "review", Reason: strings.Repeat("x", 4097)})
			return err
		},
		"changes blank feedback": func() error {
			_, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{ProjectID: "project", ReviewID: "review"})
			return err
		},
		"changes oversized feedback": func() error {
			_, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{ProjectID: "project", ReviewID: "review", Feedback: strings.Repeat("x", maxReviewFeedbackCharacters+1)})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := command(); !errors.Is(err, store.ErrInvalidArgument) {
				t.Fatalf("error=%v want ErrInvalidArgument", err)
			}
		})
	}
}

func TestRequestReviewChangesCountsUnicodeCharacters(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	fixture, review := readyReviewFixture(t, s, "unicode-feedback-limit")

	feedback := strings.Repeat("é", maxReviewFeedbackCharacters)
	result, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{
		ProjectID: fixture.project.ID,
		ReviewID:  review.ID,
		Feedback:  feedback,
	})
	if err != nil {
		t.Fatalf("RequestReviewChanges() error=%v", err)
	}
	if result.Review.Status != "CHANGES_REQUESTED" {
		t.Fatalf("review status=%s want CHANGES_REQUESTED", result.Review.Status)
	}
}

func TestReviewApprovalRejectsStaleAttempt(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	fixture, review := readyReviewFixture(t, s, "stale-approval")

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status)
		VALUES ($1, $2, $3, $4, $5, 'QUEUED')
	`, fixture.project.ID, fixture.issue.ID, fixture.run.WorkspaceID, fixture.run.AgentID, fixture.run.Attempt+1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: review.ID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("BeginReviewApproval() error=%v want conflict", err)
	}
}

func TestRequestChangesRejectsPendingApprovalIntent(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	fixture, review := readyReviewFixture(t, s, "changes-with-intent")

	if _, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: review.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{
		ProjectID: fixture.project.ID,
		ReviewID:  review.ID,
		Feedback:  "Please revise this.",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("RequestReviewChanges() error=%v want conflict", err)
	}
}

func TestCompletedReviewApprovalRejectsDifferentAcceptedRevision(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	fixture, review := readyReviewFixture(t, s, "approval-revision-mismatch")

	if _, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: review.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: review.ID, AcceptedRevision: "accepted-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: review.ID, AcceptedRevision: "accepted-b"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("CompleteReviewApproval() error=%v want conflict", err)
	}
}

func TestFailReviewApprovalRequiresPendingIntent(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	fixture, review := readyReviewFixture(t, s, "fail-without-intent")

	if _, err := s.FailReviewApproval(ctx, store.FailReviewApprovalCommand{
		ProjectID: fixture.project.ID,
		ReviewID:  review.ID,
		Reason:    "candidate invalid",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("FailReviewApproval() error=%v want conflict", err)
	}

	if _, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: review.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FailReviewApproval(ctx, store.FailReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: review.ID, Reason: "candidate invalid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FailReviewApproval(ctx, store.FailReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: review.ID, Reason: "candidate still invalid"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second FailReviewApproval() error=%v want conflict", err)
	}
}
