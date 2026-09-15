package mcpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpEvidenceRuntimeInstanceID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

type protocolBlobStore struct{}

func (protocolBlobStore) Put(context.Context, string, io.Reader) (evidence.Blob, error) {
	return evidence.Blob{}, errors.New("not implemented")
}

func (protocolBlobStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (s *protocolStore) GetRunProvenance(_ context.Context, projectID, runID string) (json.RawMessage, error) {
	if projectID != s.project.ID || runID != mcpTestRunID {
		return nil, store.ErrNotFound
	}
	repositoryPath := "/srv/agent-board/repos/private"
	value := executioncontext.Provenance{
		SchemaVersion: executioncontext.ProvenanceSchemaVersion,
		Context: executioncontext.SafeContext{
			Project: executioncontext.ProjectContext{ID: projectID, Name: "MCP Project", SourceType: store.ProjectSourceLocal, RepositoryPath: repositoryPath, DefaultBranch: "main"},
			Issue: executioncontext.IssueContext{ID: mcpTestIssueID, Key: "MCP-1", Title: "First issue", Status: "TODO"},
			Run: executioncontext.RunContext{ID: runID, Attempt: 1},
			Agent: executioncontext.AgentContext{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Name: "Agent", Engine: "opencode", EngineSettings: json.RawMessage(`{"internal":"backend-only"}`)},
			Workspace: executioncontext.WorkspaceContext{ID: "workspace-1", Path: "/var/lib/agent-board/workspaces/private", RepositoryPath: &repositoryPath, WorkingBranch: "feat/private"},
		},
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *protocolStore) ListExecutionSessionsByRun(_ context.Context, projectID, runID string, _ []string) ([]store.ExecutionSession, error) {
	if projectID != s.project.ID || runID != mcpTestRunID {
		return nil, store.ErrNotFound
	}
	now := time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)
	return []store.ExecutionSession{{
		ID: "session-1", ProjectID: projectID, RunID: runID, RuntimeInstanceID: mcpEvidenceRuntimeInstanceID,
		RunnerID: "runner-1", Status: "COMPLETED", CWD: "/var/lib/agent-board/workspaces/private",
		CommandArgv: json.RawMessage(`["sh","-c","cat /srv/agent-board/repos/private/token"]`), CreatedAt: now, UpdatedAt: now,
	}}, nil
}

func (s *protocolStore) GetRuntimeInstance(_ context.Context, projectID, runtimeInstanceID string) (store.RuntimeInstance, error) {
	if projectID != s.project.ID || runtimeInstanceID != mcpEvidenceRuntimeInstanceID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	now := time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)
	return store.RuntimeInstance{ID: runtimeInstanceID, ProjectID: projectID, RuntimeID: "runtime-1", Status: "STOPPED", RunnerStatus: "DISCONNECTED", CreatedAt: now, UpdatedAt: now}, nil
}

func (s *protocolStore) ListRunEvents(_ context.Context, projectID, runID string, after int64, _ int) ([]store.Event, error) {
	if projectID != s.project.ID || runID != mcpTestRunID {
		return nil, store.ErrNotFound
	}
	if after > 0 {
		return nil, nil
	}
	now := time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)
	sequence := int64(1)
	issueID := mcpTestIssueID
	return []store.Event{{
		ID: "event-1", SchemaVersion: 1, Type: "tool.started", OccurredAt: now, ProjectID: projectID, IssueID: &issueID, RunID: &runID, Sequence: &sequence,
		Actor: json.RawMessage(`{"type":"AGENT","issueId":"66666666-6666-6666-6666-666666666666"}`),
		Payload: json.RawMessage(`{"cwd":"/var/lib/agent-board/workspaces/private","command":["sh","-c","cat /srv/agent-board/repos/private/token"],"filePath":"/workspace/private/result.txt","storageRef":"blob://internal/event"}`),
	}}, nil
}

func (s *protocolStore) GetRawOutputChunk(_ context.Context, projectID, runID, chunkID string) (store.RawOutputChunk, error) {
	for _, chunk := range mustProtocolRawOutput(s, projectID, runID) {
		if chunk.ID == chunkID {
			return chunk, nil
		}
	}
	return store.RawOutputChunk{}, store.ErrNotFound
}

func (s *protocolStore) ListRawOutputChunks(_ context.Context, projectID, runID string) ([]store.RawOutputChunk, error) {
	return mustProtocolRawOutput(s, projectID, runID), nil
}

func mustProtocolRawOutput(s *protocolStore, projectID, runID string) []store.RawOutputChunk {
	if projectID != s.project.ID || runID != mcpTestRunID {
		return nil
	}
	now := time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)
	return []store.RawOutputChunk{{ID: "chunk-1", ProjectID: projectID, IssueID: mcpTestIssueID, RunID: runID, Stream: "stdout", Sequence: 1, StorageRef: "blob://internal/raw-output", SizeBytes: 12, CreatedAt: now}}
}

func (s *protocolStore) GetArtifact(_ context.Context, projectID, runID, artifactID string) (store.Artifact, error) {
	for _, artifact := range mustProtocolArtifacts(s, projectID, runID) {
		if artifact.ID == artifactID {
			return artifact, nil
		}
	}
	return store.Artifact{}, store.ErrNotFound
}

func (s *protocolStore) ListArtifacts(_ context.Context, projectID, runID string) ([]store.Artifact, error) {
	return mustProtocolArtifacts(s, projectID, runID), nil
}

func mustProtocolArtifacts(s *protocolStore, projectID, runID string) []store.Artifact {
	if projectID != s.project.ID || runID != mcpTestRunID {
		return nil
	}
	now := time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)
	return []store.Artifact{{ID: "artifact-1", ProjectID: projectID, IssueID: mcpTestIssueID, RunID: runID, Name: "report.json", Kind: "report", SizeBytes: 24, StorageRef: "blob://internal/artifact", SafeMetadata: json.RawMessage(`{"kind":"report"}`), CreatedAt: now}}
}

func TestMCPInspectRunResponseOmitsBackendExecutionDetails(t *testing.T) {
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

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "inspect_run", Arguments: map[string]any{"projectId": mcpTestProjectID, "runId": mcpTestRunID}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("inspect_run tool error: %+v", result.Content)
	}
	if result.StructuredContent == nil {
		t.Fatal("inspect_run returned no structured content")
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(raw)
	if !strings.Contains(payload, `"issueId":"MCP-1"`) {
		t.Fatalf("public Issue key missing from inspect_run response: %s", payload)
	}
	for _, forbidden := range []string{
		mcpTestIssueID,
		"/srv/agent-board/repos/private",
		"/var/lib/agent-board/workspaces/private",
		"/workspace/private/result.txt",
		"blob://internal",
		`"repositoryPath"`,
		`"storageRef"`,
		`"cwd"`,
		`"command"`,
		"backend-only",
	} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("inspect_run response leaked %q: %s", forbidden, payload)
		}
	}
}
