package app

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectAccessReviewReadAndApprovalUseSharedBoundary(t *testing.T) {
	const projectID = "project-review-boundary"
	review := store.Review{
		ID:             "review-1",
		ProjectID:      projectID,
		IssueID:        "issue-1",
		RunID:          "run-1",
		Status:         "PENDING",
		BaseRevision:   "base-revision",
		ReviewRevision: "review-revision",
	}
	run := store.Run{ID: "run-1", ProjectID: projectID, IssueID: "issue-1"}
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project"},
		roles: map[string]string{
			"viewer": store.ProjectRoleViewer,
			"member": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{},
		runs:   map[string]store.Run{},
	}
	access := newProjectWorkflowAccess(t, fake)
	viewer := activeProjectActor("viewer", store.DeploymentRoleMember)
	member := activeProjectActor("member", store.DeploymentRoleMember)

	reviewDB := &reviewServiceStore{
		project: store.Project{ID: projectID, SourceType: store.ProjectSourceLocal},
		run:     run,
		review:  review,
		begin:   store.BeginReviewApprovalResult{Review: review, Run: run},
	}
	applier := &reviewCandidateApplierFake{revision: "accepted-revision"}
	reviews := newReviewServiceForTest(t, reviewDB, &reviewBlobStore{values: map[string][]byte{}}, applier)

	inspection, err := access.GetReview(t.Context(), viewer, reviews, projectID, review.ID)
	if err != nil || inspection.Review.ID != review.ID {
		t.Fatalf("GetReview() inspection=%+v err=%v", inspection, err)
	}

	if _, err := access.ApproveReview(t.Context(), viewer, reviews, projectID, review.ID); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer ApproveReview() error=%v", err)
	}
	if applier.calls != 0 {
		t.Fatalf("viewer reached Review approval; apply calls=%d", applier.calls)
	}

	if _, err := access.ApproveReview(t.Context(), member, reviews, projectID, review.ID); err != nil {
		t.Fatalf("member ApproveReview() error=%v", err)
	}
	if applier.calls != 1 || reviewDB.completeCommand.ProjectID != projectID || reviewDB.completeCommand.ReviewID != review.ID || reviewDB.completeCommand.AcceptedRevision != "accepted-revision" {
		t.Fatalf("approval calls=%d command=%+v", applier.calls, reviewDB.completeCommand)
	}
}

func TestProjectAccessWorkflowWrappersFailBeforeUnavailableDependencies(t *testing.T) {
	const projectID = "project-workflow-guards"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project"},
		roles: map[string]string{
			"viewer": store.ProjectRoleViewer,
			"member": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{},
		runs:   map[string]store.Run{},
	}
	access := newProjectWorkflowAccess(t, fake)
	viewer := activeProjectActor("viewer", store.DeploymentRoleMember)
	member := activeProjectActor("member", store.DeploymentRoleMember)
	outsider := activeProjectActor("outsider", store.DeploymentRoleMember)

	if _, _, err := access.AssignIssue(t.Context(), viewer, projectID, "issue-1", "agent-1"); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer AssignIssue() error=%v", err)
	}
	answerText := "answer"
	if _, err := access.AnswerQuestion(t.Context(), viewer, nil, projectID, "question-1", store.QuestionAnswer{Kind: "TEXT", Text: &answerText}); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer AnswerQuestion() error=%v", err)
	}
	if _, err := access.RequestReviewChanges(t.Context(), viewer, nil, projectID, "review-1", "changes"); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer RequestReviewChanges() error=%v", err)
	}

	if _, err := access.AnswerQuestion(t.Context(), member, nil, projectID, "question-1", store.QuestionAnswer{Kind: "TEXT", Text: &answerText}); err == nil {
		t.Fatal("AnswerQuestion() unexpectedly accepted nil QuestionService")
	}
	if _, err := access.GetReview(t.Context(), viewer, nil, projectID, "review-1"); err == nil {
		t.Fatal("GetReview() unexpectedly accepted nil ReviewService")
	}
	if _, err := access.ApproveReview(t.Context(), member, nil, projectID, "review-1"); err == nil {
		t.Fatal("ApproveReview() unexpectedly accepted nil ReviewService")
	}
	if _, err := access.RequestReviewChanges(t.Context(), member, nil, projectID, "review-1", "changes"); err == nil {
		t.Fatal("RequestReviewChanges() unexpectedly accepted nil ReviewService")
	}

	if _, err := access.ResolveIssueUUID(t.Context(), outsider, projectID, "AB-1"); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider ResolveIssueUUID() error=%v", err)
	}
	if _, err := access.ListIssues(t.Context(), outsider, projectID); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider ListIssues() error=%v", err)
	}
	if _, err := access.GetIssue(t.Context(), outsider, projectID, "issue-1"); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider GetIssue() error=%v", err)
	}
	if _, err := access.ListRuns(t.Context(), outsider, projectID); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider ListRuns() error=%v", err)
	}
	if _, err := access.GetRun(t.Context(), outsider, projectID, "run-1"); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider GetRun() error=%v", err)
	}
	if _, err := access.ListQuestions(t.Context(), outsider, nil, projectID, store.QuestionFilter{}); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider ListQuestions() error=%v", err)
	}
	if _, err := access.GetQuestion(t.Context(), outsider, nil, projectID, "question-1"); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider GetQuestion() error=%v", err)
	}
	if _, err := access.ListReviews(t.Context(), outsider, nil, projectID, store.ReviewFilter{}); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider ListReviews() error=%v", err)
	}
	if _, err := access.GetReview(t.Context(), outsider, nil, projectID, "review-1"); appErrorCode(err) != "project_not_found" {
		t.Fatalf("outsider GetReview() error=%v", err)
	}
}

func TestConfigureProjectAccessWiresOnlySupportingStores(t *testing.T) {
	fake := newProjectAccessCRUDStore()
	services := &Services{ControlPlane: New(fake)}
	if err := configureProjectAccess(services, fake); err != nil {
		t.Fatalf("configureProjectAccess() error=%v", err)
	}
	if services.ProjectAccess == nil {
		t.Fatal("configureProjectAccess() did not wire ProjectAccess")
	}

	unsupported := &Services{ControlPlane: New(fake)}
	if err := configureProjectAccess(unsupported, struct{}{}); err != nil {
		t.Fatalf("configureProjectAccess(unsupported) error=%v", err)
	}
	if unsupported.ProjectAccess != nil {
		t.Fatal("configureProjectAccess(unsupported) unexpectedly wired ProjectAccess")
	}

	missingControlPlane := &Services{}
	if err := configureProjectAccess(missingControlPlane, fake); err == nil {
		t.Fatal("configureProjectAccess() unexpectedly accepted missing ControlPlane")
	}
}
