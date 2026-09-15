package mcpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpTestAgentID = "44444444-4444-4444-4444-444444444444"

func (s *protocolStore) ListAgents(_ context.Context, projectID *string) ([]store.Agent, error) {
	if projectID == nil || *projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	scope := s.project.ID
	return []store.Agent{{
		ID:               mcpTestAgentID,
		ProjectID:        &scope,
		Name:             "MCP Agent",
		Engine:           "test",
		ModelProfileID:   "model-profile",
		ConcurrencyLimit: 1,
		State:            "ENABLED",
	}}, nil
}

func (s *protocolStore) ListIssueAssignees(_ context.Context, projectID string) ([]store.Assignee, error) {
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	return []store.Assignee{
		{Type: "USER", ID: s.user.ID, Name: s.user.DisplayName},
		{Type: "AGENT", ID: mcpTestAgentID, Name: "MCP Agent"},
	}, nil
}

func TestMCPProjectDirectoryToolsUseCanonicalAccessPaths(t *testing.T) {
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

	for _, name := range []string{"list_agents", "list_issue_assignees"} {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      name,
				Arguments: map[string]any{"projectId": mcpTestProjectID},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("%s tool error: %+v", name, result.Content)
			}
		})
	}
}
