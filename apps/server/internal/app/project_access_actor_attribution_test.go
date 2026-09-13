package app

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

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
