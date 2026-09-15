package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *protocolStore) CreateIssueMutation(_ context.Context, input store.Issue) (store.IssueMutationResult, error) {
	if input.ProjectID != s.project.ID {
		return store.IssueMutationResult{}, store.ErrNotFound
	}
	input.ID = mcpTestIssueID
	input.Number = 1
	input.Key = "MCP-1"
	return store.IssueMutationResult{Issue: input}, nil
}

func (s *protocolStore) UpdateIssuePatchMutation(_ context.Context, patch store.IssuePatch, _ json.RawMessage) (store.IssueMutationResult, error) {
	issue, err := s.GetIssue(context.Background(), patch.ProjectID, patch.ID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if patch.Title != nil {
		issue.Title = *patch.Title
	}
	if patch.Description != nil {
		issue.Description = *patch.Description
	}
	if patch.Status != nil {
		issue.Status = *patch.Status
	}
	if patch.Priority != nil {
		issue.Priority = *patch.Priority
	}
	return store.IssueMutationResult{Issue: issue}, nil
}

func (s *protocolStore) SetIssueAssignee(_ context.Context, projectID, issueID string, target *store.Assignee, _ json.RawMessage) (store.IssueMutationResult, error) {
	issue, err := s.GetIssue(context.Background(), projectID, issueID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if target == nil {
		issue.AssigneeType = nil
		issue.AssigneeID = nil
		issue.AssigneeName = nil
		return store.IssueMutationResult{Issue: issue}, nil
	}
	issue.AssigneeType = &target.Type
	issue.AssigneeID = &target.ID
	issue.AssigneeName = &target.Name
	return store.IssueMutationResult{Issue: issue}, nil
}

func (s *protocolStore) ListIssueAssignees(_ context.Context, projectID string) ([]store.Assignee, error) {
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	return []store.Assignee{{Type: "USER", ID: mcpTestUserID, Name: s.user.DisplayName}}, nil
}

func TestMCPIssueMutationsUseSharedApplicationBoundaries(t *testing.T) {
	handler, _, _, token := newProtocolFixture(t)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "agent-board-test", Version: "v0.1.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   httpServer.URL,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: token}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	calls := []struct {
		name string
		args map[string]any
	}{
		{name: "create_issue", args: map[string]any{"projectId": mcpTestProjectID, "title": "Created through MCP", "description": "Uses the shared Issue service", "priority": 2}},
		{name: "update_issue", args: map[string]any{"projectId": mcpTestProjectID, "issueId": "MCP-1", "title": "Updated through MCP", "description": "Patched through ProjectAccess", "priority": 3}},
		{name: "set_issue_status", args: map[string]any{"projectId": mcpTestProjectID, "issueId": "MCP-1", "status": "IN_PROGRESS"}},
		{name: "set_issue_assignee", args: map[string]any{"projectId": mcpTestProjectID, "issueId": "MCP-1", "assignedTo": map[string]any{"type": "USER", "id": mcpTestUserID}}},
		{name: "list_issue_assignees", args: map[string]any{"projectId": mcpTestProjectID}},
	}

	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: call.name, Arguments: call.args})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("%s tool error: %+v", call.name, result.Content)
			}
		})
	}
}
