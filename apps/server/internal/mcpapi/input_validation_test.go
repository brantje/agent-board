package mcpapi

import (
	"context"
	"testing"
)

func TestMCPHandlersRejectInvalidEntityIDsBeforeServiceLookup(t *testing.T) {
	ctx := context.Background()
	server := &Server{}
	validRunID := "55555555-5555-5555-5555-555555555555"

	checks := []struct {
		name string
		want string
		call func() error
	}{
		{
			name: "get_agent",
			want: "invalid_argument: agentId must be a UUID",
			call: func() error {
				_, _, err := server.getAgent(ctx, nil, AgentInput{ProjectID: mcpTestProjectID, AgentID: "bad"})
				return err
			},
		},
		{
			name: "get_run",
			want: "invalid_argument: runId must be a UUID",
			call: func() error {
				_, _, err := server.getRun(ctx, nil, RunInput{ProjectID: mcpTestProjectID, RunID: "bad"})
				return err
			},
		},
		{
			name: "cancel_run",
			want: "invalid_argument: runId must be a UUID",
			call: func() error {
				_, _, err := server.cancelRun(ctx, nil, RunInput{ProjectID: mcpTestProjectID, RunID: "bad"})
				return err
			},
		},
		{
			name: "inspect_run",
			want: "invalid_argument: runId must be a UUID",
			call: func() error {
				_, _, err := server.inspectRun(ctx, nil, RunInput{ProjectID: mcpTestProjectID, RunID: "bad"})
				return err
			},
		},
		{
			name: "get_question",
			want: "invalid_argument: questionId must be a UUID",
			call: func() error {
				_, _, err := server.getQuestion(ctx, nil, QuestionInput{ProjectID: mcpTestProjectID, QuestionID: "bad"})
				return err
			},
		},
		{
			name: "answer_question",
			want: "invalid_argument: questionId must be a UUID",
			call: func() error {
				_, _, err := server.answerQuestion(ctx, nil, AnswerQuestionInput{ProjectID: mcpTestProjectID, QuestionID: "bad"})
				return err
			},
		},
		{
			name: "get_review",
			want: "invalid_argument: reviewId must be a UUID",
			call: func() error {
				_, _, err := server.getReview(ctx, nil, ReviewInput{ProjectID: mcpTestProjectID, ReviewID: "bad"})
				return err
			},
		},
		{
			name: "approve_review",
			want: "invalid_argument: reviewId must be a UUID",
			call: func() error {
				_, _, err := server.approveReview(ctx, nil, ReviewInput{ProjectID: mcpTestProjectID, ReviewID: "bad"})
				return err
			},
		},
		{
			name: "request_review_changes",
			want: "invalid_argument: reviewId must be a UUID",
			call: func() error {
				_, _, err := server.requestReviewChanges(ctx, nil, RequestReviewChangesInput{ProjectID: mcpTestProjectID, ReviewID: "bad"})
				return err
			},
		},
		{
			name: "delete_issue_relationship",
			want: "invalid_argument: relationshipId must be a UUID",
			call: func() error {
				_, _, err := server.deleteIssueRelationship(ctx, nil, DeleteRelationshipInput{ProjectID: mcpTestProjectID, RelationshipID: "bad"})
				return err
			},
		},
		{
			name: "read_run_output_chunk_run",
			want: "invalid_argument: runId must be a UUID",
			call: func() error {
				_, _, err := server.readRunOutputChunk(ctx, nil, ReadRunOutputInput{ProjectID: mcpTestProjectID, RunID: "bad", ChunkID: "bad"})
				return err
			},
		},
		{
			name: "read_run_output_chunk_chunk",
			want: "invalid_argument: chunkId must be a UUID",
			call: func() error {
				_, _, err := server.readRunOutputChunk(ctx, nil, ReadRunOutputInput{ProjectID: mcpTestProjectID, RunID: validRunID, ChunkID: "bad"})
				return err
			},
		},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			if err == nil || err.Error() != check.want {
				t.Fatalf("error = %v, want %q", err, check.want)
			}
		})
	}
}

func TestMCPProjectScopedHandlersRejectInvalidProjectIDBeforeServiceLookup(t *testing.T) {
	ctx := context.Background()
	server := &Server{}

	checks := []struct {
		name string
		call func() error
	}{
		{name: "get_project", call: func() error { _, _, err := server.getProject(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{name: "get_project_role", call: func() error { _, _, err := server.getProjectRole(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{name: "list_project_members", call: func() error { _, _, err := server.listProjectMembers(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{name: "list_agents", call: func() error { _, _, err := server.listAgents(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{name: "list_issue_assignees", call: func() error { _, _, err := server.listIssueAssignees(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{name: "list_issues", call: func() error { _, _, err := server.listIssues(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{name: "get_issue", call: func() error { _, _, err := server.getIssue(ctx, nil, IssueInput{ProjectID: "bad"}); return err }},
		{name: "create_issue", call: func() error { _, _, err := server.createIssue(ctx, nil, CreateIssueInput{ProjectID: "bad"}); return err }},
		{name: "update_issue", call: func() error { _, _, err := server.updateIssue(ctx, nil, UpdateIssueInput{ProjectID: "bad"}); return err }},
		{name: "set_issue_status", call: func() error { _, _, err := server.setIssueStatus(ctx, nil, SetIssueStatusInput{ProjectID: "bad"}); return err }},
		{name: "place_issue_on_board", call: func() error { _, _, err := server.placeIssueOnBoard(ctx, nil, PlaceIssueInput{ProjectID: "bad"}); return err }},
		{name: "set_issue_assignee", call: func() error { _, _, err := server.setIssueAssignee(ctx, nil, SetIssueAssigneeInput{ProjectID: "bad"}); return err }},
		{name: "list_issue_relationships", call: func() error { _, _, err := server.listIssueRelationships(ctx, nil, IssueInput{ProjectID: "bad"}); return err }},
		{name: "create_issue_relationship", call: func() error { _, _, err := server.createIssueRelationship(ctx, nil, CreateRelationshipInput{ProjectID: "bad"}); return err }},
		{name: "get_issue_execution_state", call: func() error { _, _, err := server.getIssueExecutionState(ctx, nil, IssueInput{ProjectID: "bad"}); return err }},
		{name: "start_issue_run", call: func() error { _, _, err := server.startIssueRun(ctx, nil, IssueInput{ProjectID: "bad"}); return err }},
		{name: "list_runs", call: func() error { _, _, err := server.listRuns(ctx, nil, ProjectInput{ProjectID: "bad"}); return err }},
		{name: "list_questions", call: func() error { _, _, err := server.listQuestions(ctx, nil, ListQuestionsInput{ProjectID: "bad"}); return err }},
		{name: "list_reviews", call: func() error { _, _, err := server.listReviews(ctx, nil, ListReviewsInput{ProjectID: "bad"}); return err }},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			if err == nil || err.Error() != "invalid_argument: projectId must be a UUID" {
				t.Fatalf("error = %v, want invalid project UUID", err)
			}
		})
	}
}
