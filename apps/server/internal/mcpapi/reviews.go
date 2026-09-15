package mcpapi

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerReviewTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool("list_reviews", "List Reviews with optional public Issue key and Review status filters."), objectListHandler(s.listReviews))
	mcp.AddTool(server, readOnlyTool("get_review", "Inspect one Review including Decision, test status, and safe Run evidence."), s.getReview)
	mcp.AddTool(server, mutationTool("approve_review", "Approve a Review as the authenticated human User through the existing delivery and Review gates.", false, false), s.approveReview)
	mcp.AddTool(server, mutationTool("request_review_changes", "Request Review changes with persisted feedback and the existing continuation behavior.", false, false), s.requestReviewChanges)
}

func (s *Server) listReviews(ctx context.Context, _ *mcp.CallToolRequest, input ListReviewsInput) (*mcp.CallToolResult, []ReviewDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, nil, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	filter := store.ReviewFilter{Statuses: input.Statuses}
	if input.IssueID != nil {
		issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, *input.IssueID)
		if err != nil {
			return nil, nil, toolError(ctx, err)
		}
		filter.IssueID = &issueID
	}
	values, err := s.services.ProjectAccess.ListReviews(ctx, actor, s.reviews, input.ProjectID, filter)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]ReviewDTO, 0, len(values))
	for _, value := range values {
		out = append(out, reviewDTO(value, keys))
	}
	return nil, out, nil
}

func (s *Server) getReview(ctx context.Context, _ *mcp.CallToolRequest, input ReviewInput) (*mcp.CallToolResult, ReviewInspectionDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, ReviewInspectionDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.ReviewID, "reviewId"); err != nil {
		return nil, ReviewInspectionDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, ReviewInspectionDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.GetReview(ctx, actor, s.reviews, input.ProjectID, input.ReviewID)
	if err != nil {
		return nil, ReviewInspectionDTO{}, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, ReviewInspectionDTO{}, toolError(ctx, err)
	}
	out := ReviewInspectionDTO{Review: reviewDTO(value.Review, keys), Evidence: evidenceDTO(value.Evidence, keys), TestStatus: string(value.TestStatus)}
	if value.Decision != nil {
		decision := decisionDTO(*value.Decision, keys)
		out.Decision = &decision
	}
	return nil, out, nil
}

func (s *Server) approveReview(ctx context.Context, _ *mcp.CallToolRequest, input ReviewInput) (*mcp.CallToolResult, ReviewDecisionDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.ReviewID, "reviewId"); err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.ApproveReview(ctx, actor, s.reviews, input.ProjectID, input.ReviewID)
	if err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	return nil, ReviewDecisionDTO{Review: reviewDTO(value.Review, keys), Decision: decisionDTO(value.Decision, keys), Run: runDTO(value.Run, keys), Issue: issueDTO(value.Issue)}, nil
}

func (s *Server) requestReviewChanges(ctx context.Context, _ *mcp.CallToolRequest, input RequestReviewChangesInput) (*mcp.CallToolResult, ReviewDecisionDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.ReviewID, "reviewId"); err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.RequestReviewChanges(ctx, actor, s.reviews, input.ProjectID, input.ReviewID, input.Feedback)
	if err != nil {
		return nil, ReviewDecisionDTO{}, toolError(ctx, err)
	}
	return nil, ReviewDecisionDTO{Review: reviewDTO(value.Review, keys), Decision: decisionDTO(value.Decision, keys), Run: runDTO(value.Run, keys), Issue: issueDTO(value.Issue)}, nil
}
