package mcpapi

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerProjectTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool("list_projects", "List Projects visible to the authenticated Agent Board User."), objectListHandler(s.listProjects))
	mcp.AddTool(server, readOnlyTool("get_project", "Read one visible Project."), s.getProject)
	mcp.AddTool(server, readOnlyTool("get_project_role", "Return the authenticated User's effective role for one Project."), s.getProjectRole)
	mcp.AddTool(server, readOnlyTool("list_project_members", "List the safe human roster with effective Project roles."), objectListHandler(s.listProjectMembers))
	mcp.AddTool(server, readOnlyTool("list_agents", "List Agent configurations visible in Project scope without secret material."), objectListHandler(s.listAgents))
	mcp.AddTool(server, readOnlyTool("get_agent", "Read one Agent configuration visible in Project scope without secret material."), s.getAgent)
	mcp.AddTool(server, readOnlyTool("list_issue_assignees", "List current valid Issue ownership targets using the canonical assignee directory."), objectListHandler(s.listIssueAssignees))
}

func (s *Server) listProjects(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, []ProjectDTO, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	values, err := s.services.ProjectAccess.ListProjects(ctx, actor)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]ProjectDTO, 0, len(values))
	for _, value := range values {
		out = append(out, projectDTO(value))
	}
	return nil, out, nil
}

func (s *Server) getProject(ctx context.Context, _ *mcp.CallToolRequest, input ProjectInput) (*mcp.CallToolResult, ProjectDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, ProjectDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, ProjectDTO{}, toolError(ctx, err)
	}
	value, _, err := s.services.ProjectAccess.GetProject(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, ProjectDTO{}, toolError(ctx, err)
	}
	return nil, projectDTO(value), nil
}

func (s *Server) getProjectRole(ctx context.Context, _ *mcp.CallToolRequest, input ProjectInput) (*mcp.CallToolResult, ProjectRoleDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, ProjectRoleDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, ProjectRoleDTO{}, toolError(ctx, err)
	}
	role, err := s.services.ProjectAccess.EffectiveRole(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, ProjectRoleDTO{}, toolError(ctx, err)
	}
	return nil, ProjectRoleDTO{Role: role}, nil
}

func (s *Server) listProjectMembers(ctx context.Context, _ *mcp.CallToolRequest, input ProjectInput) (*mcp.CallToolResult, []ProjectMemberDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, nil, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	values, err := s.services.ProjectAccess.ListProjectMembers(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]ProjectMemberDTO, 0, len(values))
	for _, value := range values {
		out = append(out, ProjectMemberDTO{UserID: value.UserID, Username: value.Username, DisplayName: value.DisplayName, Role: value.Role})
	}
	return nil, out, nil
}

func (s *Server) listAgents(ctx context.Context, _ *mcp.CallToolRequest, input ProjectInput) (*mcp.CallToolResult, []AgentDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, nil, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	values, err := s.services.ProjectAccess.ListAgents(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]AgentDTO, 0, len(values))
	for _, value := range values {
		out = append(out, agentDTO(value))
	}
	return nil, out, nil
}

func (s *Server) getAgent(ctx context.Context, _ *mcp.CallToolRequest, input AgentInput) (*mcp.CallToolResult, AgentDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, AgentDTO{}, toolError(ctx, err)
	}
	if err := requireUUID(input.AgentID, "agentId"); err != nil {
		return nil, AgentDTO{}, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, AgentDTO{}, toolError(ctx, err)
	}
	value, err := s.services.ProjectAccess.GetAgent(ctx, actor, input.ProjectID, input.AgentID)
	if err != nil {
		return nil, AgentDTO{}, toolError(ctx, err)
	}
	return nil, agentDTO(value), nil
}

func (s *Server) listIssueAssignees(ctx context.Context, _ *mcp.CallToolRequest, input ProjectInput) (*mcp.CallToolResult, []AssigneeDTO, error) {
	if err := requireUUID(input.ProjectID, "projectId"); err != nil {
		return nil, nil, toolError(ctx, err)
	}
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	values, err := s.services.ProjectAccess.ListIssueAssignees(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, nil, toolError(ctx, err)
	}
	out := make([]AssigneeDTO, 0, len(values))
	for _, value := range values {
		out = append(out, assigneeDTO(value))
	}
	return nil, out, nil
}
