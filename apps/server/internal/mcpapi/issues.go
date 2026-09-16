package mcpapi

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerIssueTools(server *mcp.Server) {
	schemas := lockedIssueToolInputSchemas()

	mcp.AddTool(server, readOnlyTool("list_issues", "List Issues in one visible Project using public Issue keys."), objectListHandler(s.listIssues))
	mcp.AddTool(server, readOnlyTool("get_issue", "Read one Issue by public Issue key."), s.getIssue)
	createIssue := mutationTool("create_issue", "Create an Issue as the authenticated human User.", false, false)
	createIssue.InputSchema = schemas.createIssue
	mcp.AddTool(server, createIssue, s.createIssue)
	updateIssue := mutationTool("update_issue", "Patch Issue title, description, and/or priority. Board status and ownership are separate tools.", false, false)
	updateIssue.InputSchema = schemas.updateIssue
	mcp.AddTool(server, updateIssue, s.updateIssue)
	setIssueStatus := mutationTool("set_issue_status", "Set explicit Issue Board status to BACKLOG, TODO, IN_PROGRESS, BLOCKED, REVIEW, or DONE. This does not change ownership.", false, true)
	setIssueStatus.InputSchema = schemas.setIssueStatus
	mcp.AddTool(server, setIssueStatus, s.setIssueStatus)
	setIssueAssignee := mutationTool("set_issue_assignee", "Set or clear Issue ownership using USER, AGENT, or SQUAD plus UUID. This does not change Board status or cancel Runs.", false, false)
	setIssueAssignee.InputSchema = schemas.setIssueAssignee
	mcp.AddTool(server, setIssueAssignee, s.setIssueAssignee)
	mcp.AddTool(server, readOnlyTool("list_issue_relationships", "List canonical relationships for an Issue by public Issue key."), objectListHandler(s.listIssueRelationships))
	createRelationship := mutationTool("create_issue_relationship", "Create blocks, depends_on, related_to, or duplicates relationship through canonical validation.", false, false)
	createRelationship.InputSchema = schemas.createRelationship
	mcp.AddTool(server, createRelationship, s.createIssueRelationship)
	mcp.AddTool(server, mutationTool("delete_issue_relationship", "Delete one Issue relationship through canonical validation.", true, false), s.deleteIssueRelationship)
	mcp.AddTool(server, readOnlyTool("get_issue_execution_state", "Read the canonical derived Issue execution state including executionAgent, canStart and activeRun."), s.getIssueExecutionState)
	mcp.AddTool(server, mutationTool("start_issue_run", "Explicitly start or run again using the Issue's current AGENT owner or current SQUAD leader. Status and ownership stay unchanged.", false, false), s.startIssueRun)
}

func (s *Server) listIssues(ctx context.Context, _ *mcp.CallToolRequest, input ProjectInput) (*mcp.CallToolResult, []IssueDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, nil, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	values, err := s.services.ProjectAccess.ListIssues(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]IssueDTO, 0, len(values))
	for _, value := range values {
		out = append(out, issueDTO(value))
	}
	return nil, out, nil
}

func (s *Server) getIssue(ctx context.Context, _ *mcp.CallToolRequest, input IssueInput) (*mcp.CallToolResult, IssueDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.GetIssue(ctx, actor, input.ProjectID, issueID)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	return nil, issueDTO(value), nil
}

func (s *Server) createIssue(ctx context.Context, _ *mcp.CallToolRequest, input CreateIssueInput) (*mcp.CallToolResult, IssueDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	priority := 0
	if input.Priority != nil {
		priority = *input.Priority
	}
	value, err := s.services.ProjectAccess.CreateIssue(ctx, actor, store.Issue{ProjectID: input.ProjectID, Title: input.Title, Description: input.Description, Priority: priority})
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	return nil, issueDTO(value), nil
}

func (s *Server) updateIssue(ctx context.Context, _ *mcp.CallToolRequest, input UpdateIssueInput) (*mcp.CallToolResult, IssueDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.PatchIssue(ctx, actor, store.IssuePatch{ProjectID: input.ProjectID, ID: issueID, Title: input.Title, Description: input.Description, Priority: input.Priority})
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	return nil, issueDTO(value), nil
}

func (s *Server) setIssueStatus(ctx context.Context, _ *mcp.CallToolRequest, input SetIssueStatusInput) (*mcp.CallToolResult, IssueDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	status := string(input.Status)
	value, err := s.services.ProjectAccess.PatchIssue(ctx, actor, store.IssuePatch{ProjectID: input.ProjectID, ID: issueID, Status: &status})
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	return nil, issueDTO(value), nil
}

func (s *Server) setIssueAssignee(ctx context.Context, _ *mcp.CallToolRequest, input SetIssueAssigneeInput) (*mcp.CallToolResult, IssueDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	var target *store.Assignee
	if input.AssignedTo != nil {
		if err := requireUUID(input.AssignedTo.ID, "assignedTo.id"); err != nil {
			return nil, IssueDTO{}, toolError(ctx, err)
		}
		target = &store.Assignee{Type: string(input.AssignedTo.Type), ID: input.AssignedTo.ID}
	}
	value, err := s.services.ProjectAccess.SetIssueAssignee(ctx, actor, input.ProjectID, issueID, target)
	if err != nil {
		return nil, IssueDTO{}, toolError(ctx, err)
	}
	return nil, issueDTO(value), nil
}

func (s *Server) listIssueRelationships(ctx context.Context, _ *mcp.CallToolRequest, input IssueInput) (*mcp.CallToolResult, []RelationshipDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, nil, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	values, err := s.services.ProjectAccess.ListIssueRelationships(ctx, actor, input.ProjectID, issueID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	keys, err := s.issueKeys(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]RelationshipDTO, 0, len(values))
	for _, value := range values {
		out = append(out, relationshipDTO(value, keys))
	}
	return nil, out, nil
}

func (s *Server) createIssueRelationship(ctx context.Context, _ *mcp.CallToolRequest, input CreateRelationshipInput) (*mcp.CallToolResult, RelationshipDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, RelationshipDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, RelationshipDTO{}, toolError(ctx, err)
	}
	sourceID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, RelationshipDTO{}, toolError(ctx, err)
	}
	targetID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.TargetIssueID)
	if err != nil {
		return nil, RelationshipDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.CreateIssueRelationship(ctx, actor, store.IssueRelationship{ProjectID: input.ProjectID, SourceIssueID: sourceID, TargetIssueID: targetID, Type: string(input.Type)})
	if err != nil {
		return nil, RelationshipDTO{}, toolError(ctx, err)
	}
	return nil, relationshipDTO(value, map[string]string{sourceID: input.IssueID, targetID: input.TargetIssueID}), nil
}

func (s *Server) deleteIssueRelationship(ctx context.Context, _ *mcp.CallToolRequest, input DeleteRelationshipInput) (*mcp.CallToolResult, MutationAck, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	if err := requireUUID(input.RelationshipID, "relationshipId"); err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	if err := s.services.ProjectAccess.DeleteIssueRelationship(ctx, actor, input.ProjectID, issueID, input.RelationshipID); err != nil {
		return nil, MutationAck{}, toolError(ctx, err)
	}
	return nil, MutationAck{Success: true}, nil
}

func (s *Server) getIssueExecutionState(ctx context.Context, _ *mcp.CallToolRequest, input IssueInput) (*mcp.CallToolResult, IssueExecutionStateDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, IssueExecutionStateDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, IssueExecutionStateDTO{}, toolError(ctx, err)
	}
	issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, IssueExecutionStateDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.GetIssueExecutionState(ctx, actor, input.ProjectID, issueID)
	if err != nil {
		return nil, IssueExecutionStateDTO{}, toolError(ctx, err)
	}
	return nil, issueExecutionStateDTO(value, map[string]string{issueID: input.IssueID}), nil
}

func (s *Server) startIssueRun(ctx context.Context, _ *mcp.CallToolRequest, input IssueInput) (*mcp.CallToolResult, RunDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	issueID, err := s.resolveIssue(ctx, actor, input.ProjectID, input.IssueID)
	if err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.StartIssueRun(ctx, actor, input.ProjectID, issueID)
	if err != nil {
		return nil, RunDTO{}, toolError(ctx, err)
	}
	return nil, runDTO(value, map[string]string{issueID: input.IssueID}), nil
}
