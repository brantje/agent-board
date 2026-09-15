package mcpapi

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerQuestionTools(server *mcp.Server) {
	schemas := lockedQuestionToolInputSchemas()
	listQuestions := readOnlyTool("list_questions", "List blocking or historical Questions with optional public Issue key, Run, and status filters.")
	listQuestions.InputSchema = schemas.listQuestions
	mcp.AddTool(server, listQuestions, objectListHandler(s.listQuestions))
	mcp.AddTool(server, readOnlyTool("get_question", "Read one Question."), s.getQuestion)
	answerQuestion := mutationTool("answer_question", "Answer a Question as the authenticated human User through the durable Decision and continuation path.", false, false)
	answerQuestion.InputSchema = schemas.answerQuestion
	mcp.AddTool(server, answerQuestion, s.answerQuestion)
}

func (s *Server) listQuestions(ctx context.Context, _ *mcp.CallToolRequest, input ListQuestionsInput) (*mcp.CallToolResult, []QuestionDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, nil, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	filter := store.QuestionFilter{Statuses: input.Statuses}
	if input.IssueID != nil {
		issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, *input.IssueID)
		if err != nil {
			return nil, nil, toolError(ctx, err)
		}
		filter.IssueID = &issueID
	}
	if input.RunID != nil {
		if err := requireUUID(*input.RunID, "runId"); err != nil {
			return nil, nil, toolError(ctx, err)
		}
		filter.RunID = input.RunID
	}
	values, err := s.services.ProjectAccess.ListQuestions(ctx, actor, s.services.Questions, input.ProjectID, filter)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]QuestionDTO, 0, len(values))
	for _, value := range values {
		out = append(out, questionDTO(value, keys))
	}
	return nil, out, nil
}

func (s *Server) getQuestion(ctx context.Context, _ *mcp.CallToolRequest, input QuestionInput) (*mcp.CallToolResult, QuestionDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, QuestionDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.QuestionID, "questionId"); err != nil {
		return nil, QuestionDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, QuestionDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.GetQuestion(ctx, actor, s.services.Questions, input.ProjectID, input.QuestionID)
	if err != nil {
		return nil, QuestionDTO{}, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, QuestionDTO{}, toolError(ctx, err)
	}
	return nil, questionDTO(value, keys), nil
}

func (s *Server) answerQuestion(ctx context.Context, _ *mcp.CallToolRequest, input AnswerQuestionInput) (*mcp.CallToolResult, AnswerQuestionDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, AnswerQuestionDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.QuestionID, "questionId"); err != nil {
		return nil, AnswerQuestionDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, AnswerQuestionDTO{}, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, AnswerQuestionDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.AnswerQuestion(ctx, actor, s.services.Questions, input.ProjectID, input.QuestionID, store.QuestionAnswer{Kind: input.Kind, Text: input.Text, OptionIDs: input.OptionIDs})
	if err != nil {
		return nil, AnswerQuestionDTO{}, toolError(ctx, err)
	}
	return nil, AnswerQuestionDTO{
		Question: questionDTO(value.Question, keys), Decision: decisionDTO(value.Decision, keys),
		Run: runDTO(value.Run, keys), ContinuationQueued: value.Job != nil,
	}, nil
}
