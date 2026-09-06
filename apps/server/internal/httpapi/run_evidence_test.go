package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	evidenceChunkID    = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	evidenceArtifactID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	evidenceRuntimeID  = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	evidenceSessionID  = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
)

type httpRunEvidenceStore struct {
	rawRef       string
	rawSize      int64
	artifactRef  string
	artifactSize int64
}

func (s *httpRunEvidenceStore) GetRun(_ context.Context, pid, id string) (store.Run, error) {
	if pid != projectID || id != runID {
		return store.Run{}, store.ErrNotFound
	}
	return store.Run{ID: runID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, Status: "READY_FOR_REVIEW"}, nil
}

func (s *httpRunEvidenceStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) {
	return json.RawMessage(`{"schemaVersion":1}`), nil
}

func (s *httpRunEvidenceStore) ListExecutionSessions(context.Context, string, []string) ([]store.ExecutionSession, error) {
	return []store.ExecutionSession{{
		ID: evidenceSessionID, ProjectID: projectID, RunID: runID, RuntimeInstanceID: evidenceRuntimeID,
		Status: "COMPLETED", CWD: "/workspace", CommandArgv: json.RawMessage(`["go","test","./..."]`),
	}}, nil
}

func (s *httpRunEvidenceStore) GetRuntimeInstance(_ context.Context, pid, id string) (store.RuntimeInstance, error) {
	if pid != projectID || id != evidenceRuntimeID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	return store.RuntimeInstance{ID: id, ProjectID: pid, RuntimeID: runtimeID, Status: "DESTROYED", RunnerStatus: "UNAVAILABLE"}, nil
}

func (s *httpRunEvidenceStore) ListRunEvents(_ context.Context, pid, id string, after int64, _ int) ([]store.Event, error) {
	if pid != projectID || id != runID || after > 0 {
		return nil, nil
	}
	one, two := int64(1), int64(2)
	runtimeInstanceID := evidenceRuntimeID
	return []store.Event{
		{ID: "11111111-aaaa-4aaa-8aaa-111111111111", SchemaVersion: 1, Type: "test.completed", ProjectID: projectID, RunID: httpEvidenceStringPtr(runID), RuntimeInstanceID: &runtimeInstanceID, Sequence: &one, Actor: store.EmptyObject, Payload: json.RawMessage(`{"status":"passed"}`)},
		{ID: "22222222-aaaa-4aaa-8aaa-222222222222", SchemaVersion: 1, Type: "file.modified", ProjectID: projectID, RunID: httpEvidenceStringPtr(runID), RuntimeInstanceID: &runtimeInstanceID, Sequence: &two, Actor: store.EmptyObject, Payload: json.RawMessage(`{"path":"README.md"}`)},
	}, nil
}

func (s *httpRunEvidenceStore) ListRawOutputChunks(context.Context, string, string) ([]store.RawOutputChunk, error) {
	digest := "sha256:raw"
	return []store.RawOutputChunk{{
		ID: evidenceChunkID, ProjectID: projectID, IssueID: issueID, RunID: runID, Stream: "STDOUT", Sequence: 1,
		StorageRef: s.rawRef, SizeBytes: s.rawSize, Digest: &digest,
	}}, nil
}

func (s *httpRunEvidenceStore) ListArtifacts(context.Context, string, string) ([]store.Artifact, error) {
	mediaType := "application/json"
	digest := "sha256:artifact"
	return []store.Artifact{{
		ID: evidenceArtifactID, ProjectID: projectID, IssueID: issueID, RunID: runID, Name: "candidate.json", Kind: "candidate_manifest",
		MediaType: &mediaType, SizeBytes: s.artifactSize, Digest: &digest, StorageRef: s.artifactRef, SafeMetadata: store.EmptyObject,
	}}, nil
}

func TestRunEvidenceRoutesExposeScopedMetadataAndContent(t *testing.T) {
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	rawBlob, err := blobs.Put(t.Context(), runID, bytes.NewBufferString("hello stdout"))
	if err != nil {
		t.Fatal(err)
	}
	artifactBlob, err := blobs.Put(t.Context(), runID, bytes.NewBufferString(`{"candidate":true}`))
	if err != nil {
		t.Fatal(err)
	}
	evidenceStore := &httpRunEvidenceStore{rawRef: rawBlob.Ref, rawSize: rawBlob.SizeBytes, artifactRef: artifactBlob.Ref, artifactSize: artifactBlob.SizeBytes}
	evidenceService, err := app.NewRunEvidenceService(evidenceStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithApplication(&app.Services{ControlPlane: app.New(&fakeControlPlaneStore{}), RunEvidence: evidenceService})

	t.Run("inspection", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/runs/"+runID+"/evidence", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{"\"tests\"", "\"fileChanges\"", "\"contentPath\"", evidenceChunkID, evidenceArtifactID} {
			if !strings.Contains(body, want) {
				t.Fatalf("response missing %s: %s", want, body)
			}
		}
		if strings.Contains(body, "blob:") || strings.Contains(body, "storageRef") {
			t.Fatalf("response exposed storage reference: %s", body)
		}
	})

	t.Run("raw output", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/runs/"+runID+"/raw-output/"+evidenceChunkID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != "hello stdout" {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("cache control=%q", rec.Header().Get("Cache-Control"))
		}
	})

	t.Run("artifact", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/runs/"+runID+"/artifacts/"+evidenceArtifactID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		body, err := io.ReadAll(rec.Result().Body)
		if err != nil || string(body) != `{"candidate":true}` {
			t.Fatalf("body=%q err=%v", body, err)
		}
		if !strings.Contains(rec.Header().Get("Content-Disposition"), "candidate.json") {
			t.Fatalf("content disposition=%q", rec.Header().Get("Content-Disposition"))
		}
	})
}

func TestSafeMediaTypeRejectsInvalidHeaderValues(t *testing.T) {
	invalid := "text/plain\r\nX-Evil: yes"
	if got := safeMediaType(&invalid); got != "application/octet-stream" {
		t.Fatalf("safeMediaType()=%q", got)
	}
}

func httpEvidenceStringPtr(value string) *string { return &value }
