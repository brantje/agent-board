package mcpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpTestIssueID        = "66666666-6666-6666-6666-666666666666"
	mcpTestSecondIssueID  = "99999999-9999-9999-9999-999999999999"
	mcpTestRunID          = "77777777-7777-7777-7777-777777777777"
	mcpTestRelationshipID = "88888888-8888-8888-8888-888888888888"
)

func (s *protocolStore) ListIssues(_ context.Context, projectID string) ([]store.Issue, error) {
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	return []store.Issue{
		{ID: mcpTestIssueID, ProjectID: projectID, Number: 1, Key: "MCP-1", Title: "First issue", Status: "TODO"},
		{ID: mcpTestSecondIssueID, ProjectID: projectID, Number: 2, Key: "MCP-2", Title: "Second issue", Status: "BACKLOG"},
	}, nil
}

func (s *protocolStore) GetIssue(_ context.Context, projectID, issueID string) (store.Issue, error) {
	issues, err := s.ListIssues(context.Background(), projectID)
	if err != nil {
		return store.Issue{}, err
	}
	for _, issue := range issues {
		if issue.ID == issueID {
			return issue, nil
		}
	}
	return store.Issue{}, store.ErrNotFound
}

func (s *protocolStore) GetIssueUUIDByKey(_ context.Context, projectID, key string) (string, error) {
	if projectID != s.project.ID {
		return "", store.ErrNotFound
	}
	switch key {
	case "MCP-1":
		return mcpTestIssueID, nil
	case "MCP-2":
		return mcpTestSecondIssueID, nil
	default:
		return "", store.ErrNotFound
	}
}

func (s *protocolStore) ListRuns(_ context.Context, projectID string) ([]store.Run, error) {
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	return []store.Run{{ID: mcpTestRunID, ProjectID: projectID, IssueID: mcpTestIssueID, Attempt: 1, Status: "SUCCEEDED"}}, nil
}

func (s *protocolStore) GetRun(_ context.Context, projectID, runID string) (store.Run, error) {
	if projectID != s.project.ID || runID != mcpTestRunID {
		return store.Run{}, store.ErrNotFound
	}
	return store.Run{ID: runID, ProjectID: projectID, IssueID: mcpTestIssueID, Attempt: 1, Status: "SUCCEEDED"}, nil
}

func (s *protocolStore) GetAgentInScope(_ context.Context, projectID *string, agentID string) (store.Agent, error) {
	if projectID == nil || *projectID != s.project.ID || agentID != mcpTestAgentID {
		return store.Agent{}, store.ErrNotFound
	}
	scope := s.project.ID
	return store.Agent{ID: agentID, ProjectID: &scope, Name: "MCP Agent", Engine: "test", ModelProfileID: "model-profile", ConcurrencyLimit: 1, State: "ENABLED"}, nil
}

func (s *protocolStore) ListIssueRelationships(_ context.Context, projectID, issueID string) ([]store.IssueRelationship, error) {
	if projectID != s.project.ID || issueID != mcpTestIssueID {
		return nil, store.ErrNotFound
	}
	return []store.IssueRelationship{{ID: mcpTestRelationshipID, ProjectID: projectID, SourceIssueID: mcpTestIssueID, TargetIssueID: mcpTestSecondIssueID, Type: "blocks"}}, nil
}

func (s *protocolStore) CreateIssueRelationship(_ context.Context, input store.IssueRelationship) (store.IssueRelationship, error) {
	if input.ProjectID != s.project.ID || input.SourceIssueID != mcpTestIssueID || input.TargetIssueID != mcpTestSecondIssueID {
		return store.IssueRelationship{}, store.ErrNotFound
	}
	input.ID = mcpTestRelationshipID
	return input, nil
}

func (s *protocolStore) DeleteIssueRelationship(_ context.Context, projectID, issueID, relationshipID string) error {
	if projectID != s.project.ID || issueID != mcpTestIssueID || relationshipID != mcpTestRelationshipID {
		return store.ErrNotFound
	}
	return nil
}

func (s *protocolStore) GetIssueExecutionState(_ context.Context, projectID, issueID string) (store.IssueExecutionState, error) {
	if projectID != s.project.ID || issueID != mcpTestIssueID {
		return store.IssueExecutionState{}, store.ErrNotFound
	}
	return store.IssueExecutionState{State: store.IssueExecutionReady, CanStart: true}, nil
}

func TestMCPReadToolsUseSharedProjectAccessBoundaries(t *testing.T) {
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
		{name: "list_issues", args: map[string]any{"projectId": mcpTestProjectID}},
		{name: "get_issue", args: map[string]any{"projectId": mcpTestProjectID, "issueId": "MCP-1"}},
		{name: "list_issue_relationships", args: map[string]any{"projectId": mcpTestProjectID, "issueId": "MCP-1"}},
		{name: "get_issue_execution_state", args: map[string]any{"projectId": mcpTestProjectID, "issueId": "MCP-1"}},
		{name: "list_runs", args: map[string]any{"projectId": mcpTestProjectID}},
		{name: "get_run", args: map[string]any{"projectId": mcpTestProjectID, "runId": mcpTestRunID}},
		{name: "get_agent", args: map[string]any{"projectId": mcpTestProjectID, "agentId": mcpTestAgentID}},
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

func TestMCPRelationshipMutationsUseSharedProjectAccessBoundaries(t *testing.T) {
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
		{name: "create_issue_relationship", args: map[string]any{"projectId": mcpTestProjectID, "issueId": "MCP-1", "targetIssueId": "MCP-2", "type": "blocks"}},
		{name: "delete_issue_relationship", args: map[string]any{"projectId": mcpTestProjectID, "issueId": "MCP-1", "relationshipId": mcpTestRelationshipID}},
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
