package mcpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpTestProjectID = "11111111-1111-1111-1111-111111111111"
	mcpTestUserID    = "22222222-2222-2222-2222-222222222222"
)

type protocolStore struct {
	store.ControlPlaneStore
	store.AuthStore
	store.ProjectAccessStore
	user     store.User
	project  store.Project
	sessions []store.AuthSession
}

func (s *protocolStore) UserCount(context.Context) (int, error) {
	if s.user.ID == "" {
		return 0, nil
	}
	return 1, nil
}

func (s *protocolStore) BootstrapUser(_ context.Context, user store.User) (store.User, error) {
	user.ID = mcpTestUserID
	user.AuthVersion = 1
	user.CreatedAt = time.Now().UTC()
	user.UpdatedAt = user.CreatedAt
	s.user = user
	return user, nil
}

func (s *protocolStore) GetUser(_ context.Context, id string) (store.User, error) {
	if id != s.user.ID {
		return store.User{}, store.ErrNotFound
	}
	return s.user, nil
}

func (s *protocolStore) GetUserByLogin(_ context.Context, login string) (store.User, error) {
	if login != s.user.Username && login != s.user.Email {
		return store.User{}, store.ErrNotFound
	}
	return s.user, nil
}

func (s *protocolStore) GetAuthSettings(context.Context) (store.AuthSettings, error) {
	return store.AuthSettings{
		AccessTokenLifetime: time.Hour, RefreshTokenLifetime: 24 * time.Hour,
		PasswordPolicy: store.PasswordPolicy{MinimumLength: 12},
	}, nil
}

func (s *protocolStore) CreateAuthSession(_ context.Context, session store.AuthSession) (store.AuthSession, error) {
	if session.ID == "" {
		session.ID = "33333333-3333-3333-3333-333333333333"
	}
	s.sessions = append(s.sessions, session)
	return session, nil
}

func (s *protocolStore) ListProjects(context.Context) ([]store.Project, error) {
	return []store.Project{s.project}, nil
}

func (s *protocolStore) GetProject(_ context.Context, projectID string) (store.Project, error) {
	if projectID != s.project.ID {
		return store.Project{}, store.ErrNotFound
	}
	return s.project, nil
}

func (s *protocolStore) ListProjectsForUser(_ context.Context, userID string) ([]store.Project, error) {
	if userID != s.user.ID {
		return nil, nil
	}
	return []store.Project{s.project}, nil
}

func (s *protocolStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	if projectID != s.project.ID || userID != s.user.ID {
		return "", store.ErrNotFound
	}
	return store.ProjectRoleAdmin, nil
}

func (s *protocolStore) ListProjectMembers(context.Context, string) ([]store.ProjectMember, error) {
	return []store.ProjectMember{{UserID: s.user.ID, Username: s.user.Username, DisplayName: s.user.DisplayName, Role: store.ProjectRoleAdmin}}, nil
}

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (t bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func newProtocolFixture(t *testing.T) (http.Handler, *protocolStore, *app.AuthService, string) {
	t.Helper()
	fake := &protocolStore{project: store.Project{ID: mcpTestProjectID, Name: "MCP Project", IssuePrefix: "MCP", SourceType: store.ProjectSourceLocal, DefaultBranch: "main", WorkflowSettings: store.EmptyObject}}
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	auth, err := app.NewAuthService(fake, app.AuthServiceConfig{Now: func() time.Time { return now }, SigningKey: bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Bootstrap(t.Context(), app.BootstrapRegistration{Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: "long-enough-password"}); err != nil {
		t.Fatal(err)
	}
	tokens, err := auth.Login(t.Context(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	controlPlane := app.New(fake)
	access, err := app.NewProjectAccessService(controlPlane, fake)
	if err != nil {
		t.Fatal(err)
	}
	runEvidence, err := app.NewRunEvidenceService(fake, protocolBlobStore{})
	if err != nil {
		t.Fatal(err)
	}
	services := &app.Services{ControlPlane: controlPlane, Auth: auth, ProjectAccess: access, RunEvidence: runEvidence}
	return newHandler(services, nil), fake, auth, tokens.AccessToken
}

func TestMCPProtocolInitializesListsExactV1ToolsAndCallsTool(t *testing.T) {
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

	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	want := []string{
		"answer_question", "approve_review", "cancel_run", "create_issue", "create_issue_relationship", "delete_issue_relationship",
		"get_agent", "get_issue", "get_issue_execution_state", "get_project", "get_project_role", "get_question", "get_review", "get_run",
		"inspect_run", "list_agents", "list_issue_assignees", "list_issue_relationships", "list_issues", "list_project_members", "list_projects",
		"list_questions", "list_reviews", "list_runs", "place_issue_on_board", "read_run_output_chunk", "request_review_changes", "set_issue_assignee", "set_issue_status",
		"start_issue_run", "update_issue",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("list_projects tool error: %+v", result.Content)
	}
}

func TestMCPAuthenticationRejectsMissingInvalidAndInactiveCredentials(t *testing.T) {
	handler, fake, _, token := newProtocolFixture(t)

	for name, authorization := range map[string]string{"missing": "", "invalid": "Bearer not-a-token"} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if authorization != "" {
				request.Header.Set("Authorization", authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
		})
	}

	fake.user.Status = store.UserStatusDisabled
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("inactive user status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestNewHandlerRequiresCompleteApplicationServices(t *testing.T) {
	if _, err := NewHandler(nil); err == nil {
		t.Fatal("nil services unexpectedly accepted")
	}
}
