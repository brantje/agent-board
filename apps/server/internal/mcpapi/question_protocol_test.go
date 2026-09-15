package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpTestQuestionID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	mcpTestDecisionID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

func (s *protocolStore) CreateQuestion(_ context.Context, question store.Question) (store.Question, error) {
	return question, nil
}

func (s *protocolStore) GetQuestion(_ context.Context, projectID, questionID string) (store.Question, error) {
	if projectID != s.project.ID || questionID != mcpTestQuestionID {
		return store.Question{}, store.ErrNotFound
	}
	return protocolQuestion(projectID), nil
}

func (s *protocolStore) ListQuestions(_ context.Context, projectID string, filter store.QuestionFilter) ([]store.Question, error) {
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	if filter.IssueID != nil && *filter.IssueID != mcpTestIssueID {
		return nil, store.ErrNotFound
	}
	if filter.RunID != nil && *filter.RunID != mcpTestRunID {
		return nil, store.ErrNotFound
	}
	return []store.Question{protocolQuestion(projectID)}, nil
}

func (s *protocolStore) GetDecisionByQuestion(_ context.Context, projectID, questionID string) (store.Decision, error) {
	if projectID != s.project.ID || questionID != mcpTestQuestionID {
		return store.Decision{}, store.ErrNotFound
	}
	return protocolDecision(projectID), nil
}

func (s *protocolStore) GetOpenBlockingQuestion(_ context.Context, projectID, runID string) (store.Question, error) {
	if projectID != s.project.ID || runID != mcpTestRunID {
		return store.Question{}, store.ErrNotFound
	}
	return protocolQuestion(projectID), nil
}

func (s *protocolStore) AnswerQuestion(_ context.Context, command store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	if command.ProjectID != s.project.ID || command.QuestionID != mcpTestQuestionID || command.ActorType != store.ActorTypeHuman || command.ActorID == nil || *command.ActorID != mcpTestUserID {
		return store.AnswerQuestionResult{}, store.ErrNotFound
	}
	question := protocolQuestion(command.ProjectID)
	question.Status = "ANSWERED"
	decision := protocolDecision(command.ProjectID)
	return store.AnswerQuestionResult{
		Question: question,
		Decision: decision,
		Run: store.Run{ID: mcpTestRunID, ProjectID: command.ProjectID, IssueID: mcpTestIssueID, Attempt: 1, Status: "WAITING_FOR_INPUT"},
	}, nil
}

func protocolQuestion(projectID string) store.Question {
	return store.Question{
		ID: mcpTestQuestionID, ProjectID: projectID, IssueID: mcpTestIssueID, RunID: mcpTestRunID,
		Prompt: "Choose a direction", Kind: "TEXT", Options: json.RawMessage(`[]`), Blocking: true, Status: "OPEN",
	}
}

func protocolDecision(projectID string) store.Decision {
	issueID, runID, questionID, actorID := mcpTestIssueID, mcpTestRunID, mcpTestQuestionID, mcpTestUserID
	return store.Decision{
		ID: mcpTestDecisionID, ProjectID: projectID, IssueID: &issueID, RunID: &runID, QuestionID: &questionID,
		Kind: "QUESTION", Outcome: "ANSWERED", ActorType: store.ActorTypeHuman, ActorID: &actorID, SafeDetails: json.RawMessage(`{}`),
	}
}

func newQuestionProtocolFixture(t *testing.T) (http.Handler, string) {
	t.Helper()
	_, fake, auth, token := newProtocolFixture(t)
	controlPlane := app.New(fake)
	access, err := app.NewProjectAccessService(controlPlane, fake)
	if err != nil {
		t.Fatal(err)
	}
	questions, err := app.NewQuestionService(fake)
	if err != nil {
		t.Fatal(err)
	}
	services := &app.Services{ControlPlane: controlPlane, Auth: auth, ProjectAccess: access, Questions: questions}
	return newHandler(services, nil), token
}

func TestMCPQuestionToolsUseSharedQuestionService(t *testing.T) {
	handler, token := newQuestionProtocolFixture(t)
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

	issueKey := "MCP-1"
	calls := []struct {
		name string
		args map[string]any
	}{
		{name: "list_questions", args: map[string]any{"projectId": mcpTestProjectID, "issueId": issueKey, "runId": mcpTestRunID, "statuses": []string{"OPEN"}}},
		{name: "get_question", args: map[string]any{"projectId": mcpTestProjectID, "questionId": mcpTestQuestionID}},
		{name: "answer_question", args: map[string]any{"projectId": mcpTestProjectID, "questionId": mcpTestQuestionID, "kind": "TEXT", "text": "Use the shared path"}},
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
