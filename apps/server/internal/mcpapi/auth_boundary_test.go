package mcpapi

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	authBoundaryProjectID      = "11111111-1111-4111-8111-111111111111"
	authBoundaryAgentID        = "22222222-2222-4222-8222-222222222222"
	authBoundaryRunID          = "33333333-3333-4333-8333-333333333333"
	authBoundaryChunkID        = "44444444-4444-4444-8444-444444444444"
	authBoundaryQuestionID     = "55555555-5555-4555-8555-555555555555"
	authBoundaryReviewID       = "66666666-6666-4666-8666-666666666666"
	authBoundaryRelationshipID = "77777777-7777-4777-8777-777777777777"
	authBoundaryIssueKey       = "MCP-1"
)

func TestMCPToolHandlersRequireAuthenticatedActor(t *testing.T) {
	server := &Server{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "list_projects", call: func() error { _, _, err := server.listProjects(ctx, nil, EmptyInput{}); return err }},
		{name: "get_project", call: func() error { _, _, err := server.getProject(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_project_role", call: func() error { _, _, err := server.getProjectRole(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "list_project_members", call: func() error { _, _, err := server.listProjectMembers(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "list_agents", call: func() error { _, _, err := server.listAgents(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_agent", call: func() error { _, _, err := server.getAgent(ctx, nil, AgentInput{ProjectID: authBoundaryProjectID, AgentID: authBoundaryAgentID}); return err }},
		{name: "list_issue_assignees", call: func() error { _, _, err := server.listIssueAssignees(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "list_issues", call: func() error { _, _, err := server.listIssues(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_issue", call: func() error { _, _, err := server.getIssue(ctx, nil, IssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "create_issue", call: func() error { _, _, err := server.createIssue(ctx, nil, CreateIssueInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "update_issue", call: func() error { _, _, err := server.updateIssue(ctx, nil, UpdateIssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "set_issue_status", call: func() error { _, _, err := server.setIssueStatus(ctx, nil, SetIssueStatusInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "set_issue_assignee", call: func() error { _, _, err := server.setIssueAssignee(ctx, nil, SetIssueAssigneeInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "list_issue_relationships", call: func() error { _, _, err := server.listIssueRelationships(ctx, nil, IssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "create_issue_relationship", call: func() error { _, _, err := server.createIssueRelationship(ctx, nil, CreateRelationshipInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey, TargetIssueID: "MCP-2"}); return err }},
		{name: "delete_issue_relationship", call: func() error { _, _, err := server.deleteIssueRelationship(ctx, nil, DeleteRelationshipInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey, RelationshipID: authBoundaryRelationshipID}); return err }},
		{name: "get_issue_execution_state", call: func() error { _, _, err := server.getIssueExecutionState(ctx, nil, IssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "start_issue_run", call: func() error { _, _, err := server.startIssueRun(ctx, nil, IssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "list_runs", call: func() error { _, _, err := server.listRuns(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_run", call: func() error { _, _, err := server.getRun(ctx, nil, RunInput{ProjectID: authBoundaryProjectID, RunID: authBoundaryRunID}); return err }},
		{name: "cancel_run", call: func() error { _, _, err := server.cancelRun(ctx, nil, RunInput{ProjectID: authBoundaryProjectID, RunID: authBoundaryRunID}); return err }},
		{name: "inspect_run", call: func() error { _, _, err := server.inspectRun(ctx, nil, RunInput{ProjectID: authBoundaryProjectID, RunID: authBoundaryRunID}); return err }},
		{name: "read_run_output_chunk", call: func() error { _, _, err := server.readRunOutputChunk(ctx, nil, ReadRunOutputInput{ProjectID: authBoundaryProjectID, RunID: authBoundaryRunID, ChunkID: authBoundaryChunkID}); return err }},
		{name: "list_questions", call: func() error { _, _, err := server.listQuestions(ctx, nil, ListQuestionsInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_question", call: func() error { _, _, err := server.getQuestion(ctx, nil, QuestionInput{ProjectID: authBoundaryProjectID, QuestionID: authBoundaryQuestionID}); return err }},
		{name: "answer_question", call: func() error { _, _, err := server.answerQuestion(ctx, nil, AnswerQuestionInput{ProjectID: authBoundaryProjectID, QuestionID: authBoundaryQuestionID}); return err }},
		{name: "list_reviews", call: func() error { _, _, err := server.listReviews(ctx, nil, ListReviewsInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_review", call: func() error { _, _, err := server.getReview(ctx, nil, ReviewInput{ProjectID: authBoundaryProjectID, ReviewID: authBoundaryReviewID}); return err }},
		{name: "approve_review", call: func() error { _, _, err := server.approveReview(ctx, nil, ReviewInput{ProjectID: authBoundaryProjectID, ReviewID: authBoundaryReviewID}); return err }},
		{name: "request_review_changes", call: func() error { _, _, err := server.requestReviewChanges(ctx, nil, RequestReviewChangesInput{ProjectID: authBoundaryProjectID, ReviewID: authBoundaryReviewID}); return err }},
	}

	if len(tests) != 30 {
		t.Fatalf("tool test inventory = %d, want 30", len(tests))
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil {
				t.Fatal("handler accepted a request without an authenticated actor")
			}
		})
	}
}

func TestMCPProjectScopedToolsPreserveNotFoundIsolation(t *testing.T) {
	fake := &protocolStore{
		user: store.User{
			ID:             mcpTestUserID,
			Username:       "member",
			DeploymentRole: store.DeploymentRoleMember,
			Status:         store.UserStatusActive,
		},
		project: store.Project{ID: authBoundaryProjectID, Name: "Project", IssuePrefix: "MCP"},
	}
	controlPlane := app.New(fake)
	access, err := app.NewProjectAccessService(controlPlane, fake)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{services: &app.Services{ControlPlane: controlPlane, ProjectAccess: access}}
	ctx := context.WithValue(context.Background(), actorContextKey{}, app.AuthenticatedUser{
		ID:             "88888888-8888-4888-8888-888888888888",
		Username:       "outsider",
		DeploymentRole: store.DeploymentRoleMember,
		Status:         store.UserStatusActive,
	})

	tests := []struct {
		name string
		call func() error
	}{
		{name: "get_project", call: func() error { _, _, err := server.getProject(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_project_role", call: func() error { _, _, err := server.getProjectRole(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "list_project_members", call: func() error { _, _, err := server.listProjectMembers(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "list_agents", call: func() error { _, _, err := server.listAgents(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_agent", call: func() error { _, _, err := server.getAgent(ctx, nil, AgentInput{ProjectID: authBoundaryProjectID, AgentID: authBoundaryAgentID}); return err }},
		{name: "list_issue_assignees", call: func() error { _, _, err := server.listIssueAssignees(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "list_issues", call: func() error { _, _, err := server.listIssues(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_issue", call: func() error { _, _, err := server.getIssue(ctx, nil, IssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "create_issue", call: func() error { _, _, err := server.createIssue(ctx, nil, CreateIssueInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "update_issue", call: func() error { _, _, err := server.updateIssue(ctx, nil, UpdateIssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "set_issue_status", call: func() error { _, _, err := server.setIssueStatus(ctx, nil, SetIssueStatusInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "set_issue_assignee", call: func() error { _, _, err := server.setIssueAssignee(ctx, nil, SetIssueAssigneeInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "list_issue_relationships", call: func() error { _, _, err := server.listIssueRelationships(ctx, nil, IssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "create_issue_relationship", call: func() error { _, _, err := server.createIssueRelationship(ctx, nil, CreateRelationshipInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey, TargetIssueID: "MCP-2"}); return err }},
		{name: "delete_issue_relationship", call: func() error { _, _, err := server.deleteIssueRelationship(ctx, nil, DeleteRelationshipInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey, RelationshipID: authBoundaryRelationshipID}); return err }},
		{name: "get_issue_execution_state", call: func() error { _, _, err := server.getIssueExecutionState(ctx, nil, IssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "start_issue_run", call: func() error { _, _, err := server.startIssueRun(ctx, nil, IssueInput{ProjectID: authBoundaryProjectID, IssueID: authBoundaryIssueKey}); return err }},
		{name: "list_runs", call: func() error { _, _, err := server.listRuns(ctx, nil, ProjectInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_run", call: func() error { _, _, err := server.getRun(ctx, nil, RunInput{ProjectID: authBoundaryProjectID, RunID: authBoundaryRunID}); return err }},
		{name: "cancel_run", call: func() error { _, _, err := server.cancelRun(ctx, nil, RunInput{ProjectID: authBoundaryProjectID, RunID: authBoundaryRunID}); return err }},
		{name: "inspect_run", call: func() error { _, _, err := server.inspectRun(ctx, nil, RunInput{ProjectID: authBoundaryProjectID, RunID: authBoundaryRunID}); return err }},
		{name: "read_run_output_chunk", call: func() error { _, _, err := server.readRunOutputChunk(ctx, nil, ReadRunOutputInput{ProjectID: authBoundaryProjectID, RunID: authBoundaryRunID, ChunkID: authBoundaryChunkID}); return err }},
		{name: "list_questions", call: func() error { _, _, err := server.listQuestions(ctx, nil, ListQuestionsInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_question", call: func() error { _, _, err := server.getQuestion(ctx, nil, QuestionInput{ProjectID: authBoundaryProjectID, QuestionID: authBoundaryQuestionID}); return err }},
		{name: "answer_question", call: func() error { _, _, err := server.answerQuestion(ctx, nil, AnswerQuestionInput{ProjectID: authBoundaryProjectID, QuestionID: authBoundaryQuestionID}); return err }},
		{name: "list_reviews", call: func() error { _, _, err := server.listReviews(ctx, nil, ListReviewsInput{ProjectID: authBoundaryProjectID}); return err }},
		{name: "get_review", call: func() error { _, _, err := server.getReview(ctx, nil, ReviewInput{ProjectID: authBoundaryProjectID, ReviewID: authBoundaryReviewID}); return err }},
		{name: "approve_review", call: func() error { _, _, err := server.approveReview(ctx, nil, ReviewInput{ProjectID: authBoundaryProjectID, ReviewID: authBoundaryReviewID}); return err }},
		{name: "request_review_changes", call: func() error { _, _, err := server.requestReviewChanges(ctx, nil, RequestReviewChangesInput{ProjectID: authBoundaryProjectID, ReviewID: authBoundaryReviewID}); return err }},
	}

	if len(tests) != 29 {
		t.Fatalf("scoped tool test inventory = %d, want 29", len(tests))
	}
	const want = "project_not_found: project not found"
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if err == nil || err.Error() != want {
				t.Fatalf("handler returned %v; want %s", err, want)
			}
		})
	}
}
