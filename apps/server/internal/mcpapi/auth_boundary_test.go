package mcpapi

import (
	"context"
	"testing"
)

func TestMCPToolHandlersRequireAuthenticatedActor(t *testing.T) {
	const (
		projectID      = "11111111-1111-4111-8111-111111111111"
		agentID        = "22222222-2222-4222-8222-222222222222"
		runID          = "33333333-3333-4333-8333-333333333333"
		chunkID        = "44444444-4444-4444-8444-444444444444"
		questionID     = "55555555-5555-4555-8555-555555555555"
		reviewID       = "66666666-6666-4666-8666-666666666666"
		relationshipID = "77777777-7777-4777-8777-777777777777"
		issueKey       = "MCP-1"
	)

	server := &Server{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "list_projects", call: func() error { _, _, err := server.listProjects(ctx, nil, EmptyInput{}); return err }},
		{name: "get_project", call: func() error { _, _, err := server.getProject(ctx, nil, ProjectInput{ProjectID: projectID}); return err }},
		{name: "get_project_role", call: func() error { _, _, err := server.getProjectRole(ctx, nil, ProjectInput{ProjectID: projectID}); return err }},
		{name: "list_project_members", call: func() error { _, _, err := server.listProjectMembers(ctx, nil, ProjectInput{ProjectID: projectID}); return err }},
		{name: "list_agents", call: func() error { _, _, err := server.listAgents(ctx, nil, ProjectInput{ProjectID: projectID}); return err }},
		{name: "get_agent", call: func() error { _, _, err := server.getAgent(ctx, nil, AgentInput{ProjectID: projectID, AgentID: agentID}); return err }},
		{name: "list_issue_assignees", call: func() error { _, _, err := server.listIssueAssignees(ctx, nil, ProjectInput{ProjectID: projectID}); return err }},
		{name: "list_issues", call: func() error { _, _, err := server.listIssues(ctx, nil, ProjectInput{ProjectID: projectID}); return err }},
		{name: "get_issue", call: func() error { _, _, err := server.getIssue(ctx, nil, IssueInput{ProjectID: projectID, IssueID: issueKey}); return err }},
		{name: "create_issue", call: func() error { _, _, err := server.createIssue(ctx, nil, CreateIssueInput{ProjectID: projectID}); return err }},
		{name: "update_issue", call: func() error { _, _, err := server.updateIssue(ctx, nil, UpdateIssueInput{ProjectID: projectID, IssueID: issueKey}); return err }},
		{name: "set_issue_status", call: func() error { _, _, err := server.setIssueStatus(ctx, nil, SetIssueStatusInput{ProjectID: projectID, IssueID: issueKey}); return err }},
		{name: "set_issue_assignee", call: func() error { _, _, err := server.setIssueAssignee(ctx, nil, SetIssueAssigneeInput{ProjectID: projectID, IssueID: issueKey}); return err }},
		{name: "list_issue_relationships", call: func() error { _, _, err := server.listIssueRelationships(ctx, nil, IssueInput{ProjectID: projectID, IssueID: issueKey}); return err }},
		{name: "create_issue_relationship", call: func() error { _, _, err := server.createIssueRelationship(ctx, nil, CreateRelationshipInput{ProjectID: projectID, IssueID: issueKey, TargetIssueID: "MCP-2"}); return err }},
		{name: "delete_issue_relationship", call: func() error { _, _, err := server.deleteIssueRelationship(ctx, nil, DeleteRelationshipInput{ProjectID: projectID, IssueID: issueKey, RelationshipID: relationshipID}); return err }},
		{name: "get_issue_execution_state", call: func() error { _, _, err := server.getIssueExecutionState(ctx, nil, IssueInput{ProjectID: projectID, IssueID: issueKey}); return err }},
		{name: "start_issue_run", call: func() error { _, _, err := server.startIssueRun(ctx, nil, IssueInput{ProjectID: projectID, IssueID: issueKey}); return err }},
		{name: "list_runs", call: func() error { _, _, err := server.listRuns(ctx, nil, ProjectInput{ProjectID: projectID}); return err }},
		{name: "get_run", call: func() error { _, _, err := server.getRun(ctx, nil, RunInput{ProjectID: projectID, RunID: runID}); return err }},
		{name: "cancel_run", call: func() error { _, _, err := server.cancelRun(ctx, nil, RunInput{ProjectID: projectID, RunID: runID}); return err }},
		{name: "inspect_run", call: func() error { _, _, err := server.inspectRun(ctx, nil, RunInput{ProjectID: projectID, RunID: runID}); return err }},
		{name: "read_run_output_chunk", call: func() error { _, _, err := server.readRunOutputChunk(ctx, nil, ReadRunOutputInput{ProjectID: projectID, RunID: runID, ChunkID: chunkID}); return err }},
		{name: "list_questions", call: func() error { _, _, err := server.listQuestions(ctx, nil, ListQuestionsInput{ProjectID: projectID}); return err }},
		{name: "get_question", call: func() error { _, _, err := server.getQuestion(ctx, nil, QuestionInput{ProjectID: projectID, QuestionID: questionID}); return err }},
		{name: "answer_question", call: func() error { _, _, err := server.answerQuestion(ctx, nil, AnswerQuestionInput{ProjectID: projectID, QuestionID: questionID}); return err }},
		{name: "list_reviews", call: func() error { _, _, err := server.listReviews(ctx, nil, ListReviewsInput{ProjectID: projectID}); return err }},
		{name: "get_review", call: func() error { _, _, err := server.getReview(ctx, nil, ReviewInput{ProjectID: projectID, ReviewID: reviewID}); return err }},
		{name: "approve_review", call: func() error { _, _, err := server.approveReview(ctx, nil, ReviewInput{ProjectID: projectID, ReviewID: reviewID}); return err }},
		{name: "request_review_changes", call: func() error { _, _, err := server.requestReviewChanges(ctx, nil, RequestReviewChangesInput{ProjectID: projectID, ReviewID: reviewID}); return err }},
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
