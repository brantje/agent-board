package mcpapi

import (
	"errors"
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	services *app.Services
	reviews  *app.ReviewService
}

func NewHandler(services *app.Services) (http.Handler, error) {
	if services == nil || services.ControlPlane == nil || services.Auth == nil || services.ProjectAccess == nil || services.Questions == nil || services.RunEvidence == nil {
		return nil, errors.New("mcp: complete application services are required")
	}
	reviews := app.ReviewServiceFromServices(services)
	if reviews == nil {
		return nil, errors.New("mcp: review service is required")
	}
	return newHandler(services, reviews), nil
}

func newHandler(services *app.Services, reviews *app.ReviewService) http.Handler {
	transport := &Server{services: services, reviews: reviews}
	server := mcp.NewServer(
		&mcp.Implementation{Name: "agent-board", Version: "v0.1.0"},
		&mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}},
	)
	transport.registerProjectTools(server)
	transport.registerIssueTools(server)
	transport.registerRunTools(server)
	transport.registerQuestionTools(server)
	transport.registerReviewTools(server)
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	return transport.authenticate(streamable)
}

func readOnlyTool(name, description string) *mcp.Tool {
	return &mcp.Tool{
		Name: name, Description: description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}
}

func mutationTool(name, description string, destructive, idempotent bool) *mcp.Tool {
	return &mcp.Tool{
		Name: name, Description: description,
		Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, IdempotentHint: idempotent},
	}
}
