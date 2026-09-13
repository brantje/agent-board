package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type actorRecordingReviewStore struct {
	*reviewServiceStore
	beginCommand store.BeginReviewApprovalCommand
}

func (s *actorRecordingReviewStore) BeginReviewApproval(ctx context.Context, command store.BeginReviewApprovalCommand) (store.BeginReviewApprovalResult, error) {
	s.beginCommand = command
	return s.reviewServiceStore.BeginReviewApproval(ctx, command)
}

func TestProjectAccessHumanActionsUseAuthenticatedUserID(t *testing.T) {
	const projectID = "project-1"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project"},
		roles: map[string]string{
			"member-user": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{},
		runs:   map[string]store.Run{},
	}
	access := newProjectWorkflowAccess(t, fake)
	actor := activeProjectActor("member-user", store.DeploymentRoleMember)

	questionDB := &questionServiceStore{}
	questions, err := NewQuestionService(questionDB)
	if err != nil {
		t.Fatal(err)
	}
	answerText := "Use the authenticated actor"
	if _, err := access.AnswerQuestion(t.Context(), actor, questions, projectID, "question-1", store.QuestionAnswer{Kind: "TEXT", Text: &answerText}); err != nil {
		t.Fatalf("AnswerQuestion() error=%v", err)
	}
	if questionDB.lastCommand.ActorID == nil || *questionDB.lastCommand.ActorID != actor.ID {
		t.Fatalf("question actor=%v want=%q", questionDB.lastCommand.ActorID, actor.ID)
	}

	reviewDB := &reviewServiceStore{}
	reviews := &ReviewService{store: reviewDB}
	if _, err := access.RequestReviewChanges(t.Context(), actor, reviews, projectID, "review-1", "Please address the edge case"); err != nil {
		t.Fatalf("RequestReviewChanges() error=%v", err)
	}
	if reviewDB.requestCommand.ActorID == nil || *reviewDB.requestCommand.ActorID != actor.ID {
		t.Fatalf("review actor=%v want=%q", reviewDB.requestCommand.ActorID, actor.ID)
	}
}

func TestProjectAccessReviewApprovalUsesAuthenticatedUserID(t *testing.T) {
	const projectID = "project-1"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project"},
		roles: map[string]string{
			"member-user": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{},
		runs:   map[string]store.Run{},
	}
	access := newProjectWorkflowAccess(t, fake)
	actor := activeProjectActor("member-user", store.DeploymentRoleMember)

	run := store.Run{ID: "run-1", ProjectID: projectID, IssueID: "issue-1"}
	review := store.Review{
		ID: "review-1", ProjectID: projectID, IssueID: "issue-1", RunID: run.ID, Status: "PENDING",
		BaseRevision: "base-revision", ReviewRevision: "review-revision",
	}
	base := &reviewServiceStore{
		project: store.Project{ID: projectID, SourceType: store.ProjectSourceLocal, RepositoryPath: "/repo", DefaultBranch: "main"},
		run:     run,
		review:  review,
		begin:   store.BeginReviewApprovalResult{Review: review, Run: run},
		complete: store.CompleteReviewApprovalResult{
			Review: store.Review{ID: review.ID, Status: "APPROVED"},
			Run:    store.Run{ID: run.ID, Status: "COMPLETED"},
			Issue:  store.Issue{ID: "issue-1", Status: "DONE"},
		},
	}
	reviewDB := &actorRecordingReviewStore{reviewServiceStore: base}
	evidence, err := NewRunEvidenceService(reviewDB, &reviewBlobStore{values: map[string][]byte{}})
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := NewReviewService(reviewDB, reviewDB, evidence, &reviewCandidateApplierFake{revision: "accepted-revision"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := access.ApproveReview(t.Context(), actor, reviews, projectID, review.ID); err != nil {
		t.Fatalf("ApproveReview() error=%v", err)
	}
	if reviewDB.beginCommand.ActorID == nil || *reviewDB.beginCommand.ActorID != actor.ID {
		t.Fatalf("approval actor=%v want=%q", reviewDB.beginCommand.ActorID, actor.ID)
	}
}
