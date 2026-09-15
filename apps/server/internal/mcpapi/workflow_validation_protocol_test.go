package mcpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPWorkflowFiltersRejectValuesOutsideAdvertisedDomains(t *testing.T) {
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

	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{name: "list_questions", args: map[string]any{"projectId": mcpTestProjectID, "statuses": []string{"UNKNOWN"}}},
		{name: "list_reviews", args: map[string]any{"projectId": mcpTestProjectID, "statuses": []string{"UNKNOWN"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: test.name, Arguments: test.args})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("%s accepted status outside its advertised closed domain: %+v", test.name, result.StructuredContent)
			}
			raw, err := json.Marshal(result.Content)
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			if !strings.Contains(text, "validating") || !strings.Contains(text, "statuses") {
				t.Fatalf("%s returned non-validation error %s", test.name, text)
			}
		})
	}
}
