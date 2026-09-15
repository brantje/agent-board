package mcpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPCreateIssueUsesSharedBacklogDefault(t *testing.T) {
	handler, _, _, token := newProtocolFixture(t)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "agent-board-test", Version: "v0.1.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: token}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "create_issue",
		Arguments: map[string]any{
			"projectId": mcpTestProjectID,
			"title":     "Created without transport-owned status",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("create_issue tool error: %+v", result.Content)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var issue IssueDTO
	if err := json.Unmarshal(raw, &issue); err != nil {
		t.Fatal(err)
	}
	if issue.Status != "BACKLOG" {
		t.Fatalf("created Issue status = %q, want BACKLOG", issue.Status)
	}
}
