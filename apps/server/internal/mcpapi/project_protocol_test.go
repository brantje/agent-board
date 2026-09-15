package mcpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPProjectReadToolsUseExistingAccessBoundary(t *testing.T) {
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

	for _, call := range []struct {
		name      string
		arguments map[string]any
	}{
		{name: "get_project", arguments: map[string]any{"projectId": mcpTestProjectID}},
		{name: "get_project_role", arguments: map[string]any{"projectId": mcpTestProjectID}},
		{name: "list_project_members", arguments: map[string]any{"projectId": mcpTestProjectID}},
	} {
		t.Run(call.name, func(t *testing.T) {
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: call.name, Arguments: call.arguments})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("%s tool error: %+v", call.name, result.Content)
			}
		})
	}
}

func TestMCPAuthHelpersEnforceBearerAndActorContext(t *testing.T) {
	for name, value := range map[string]string{
		"missing":       "",
		"wrong_scheme":  "Basic token",
		"missing_token": "Bearer",
		"extra_fields":  "Bearer token extra",
	} {
		t.Run(name, func(t *testing.T) {
			if token, ok := bearerToken(value); ok || token != "" {
				t.Fatalf("bearerToken(%q) = %q, %t", value, token, ok)
			}
		})
	}
	if token, ok := bearerToken("bearer access-token"); !ok || token != "access-token" {
		t.Fatalf("case-insensitive bearer token = %q, %t", token, ok)
	}

	if _, err := actorFromContext(context.Background()); err == nil {
		t.Fatal("missing actor unexpectedly accepted")
	}
	if _, err := actorFromContext(context.WithValue(context.Background(), actorContextKey{}, app.AuthenticatedUser{})); err == nil {
		t.Fatal("empty actor unexpectedly accepted")
	}
	actor := app.AuthenticatedUser{ID: mcpTestUserID}
	got, err := actorFromContext(context.WithValue(context.Background(), actorContextKey{}, actor))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != actor.ID {
		t.Fatalf("actor ID = %q, want %q", got.ID, actor.ID)
	}
}

func TestNewHandlerRejectsEachIncompleteApplicationBoundary(t *testing.T) {
	_, _, auth, _ := newProtocolFixture(t)
	fake := &protocolStore{}
	controlPlane := app.New(fake)
	access, err := app.NewProjectAccessService(controlPlane, fake)
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name     string
		services *app.Services
		want     string
	}{
		{
			name:     "control_plane_auth_or_access",
			services: &app.Services{},
			want:     "mcp: control-plane authentication and Project access services are required",
		},
		{
			name: "questions_or_run_evidence",
			services: &app.Services{
				ControlPlane:  controlPlane,
				Auth:          auth,
				ProjectAccess: access,
			},
			want: "mcp: Question and Run evidence services are required",
		},
		{
			name: "review_service",
			services: &app.Services{
				ControlPlane:  controlPlane,
				Auth:          auth,
				ProjectAccess: access,
				Questions:     &app.QuestionService{},
				RunEvidence:   &app.RunEvidenceService{},
			},
			want: "mcp: Review service is required",
		},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			_, err := NewHandler(check.services)
			if err == nil || err.Error() != check.want {
				t.Fatalf("error = %v, want %q", err, check.want)
			}
		})
	}
}
