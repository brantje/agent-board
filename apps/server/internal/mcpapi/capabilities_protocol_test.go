package mcpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPInitializeAdvertisesToolsOnly(t *testing.T) {
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

	initialized := session.InitializeResult()
	if initialized == nil || initialized.Capabilities == nil {
		t.Fatal("MCP initialize returned no server capabilities")
	}
	raw, err := json.Marshal(initialized.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	var capabilities map[string]any
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		t.Fatal(err)
	}
	tools, ok := capabilities["tools"].(map[string]any)
	if !ok {
		t.Fatalf("tools capability missing: %s", raw)
	}
	if len(tools) != 0 {
		t.Fatalf("tools capability advertises unsupported options: %#v", tools)
	}
	for _, unsupported := range []string{"logging", "prompts", "resources", "completions"} {
		if _, ok := capabilities[unsupported]; ok {
			t.Fatalf("initialize advertises unsupported %s capability: %s", unsupported, raw)
		}
	}
}
