package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runEvidenceTestStore struct {
	run        store.Run
	provenance json.RawMessage
	sessions   []store.ExecutionSession
	instances  map[string]store.RuntimeInstance
	events     []store.Event
	chunks     []store.RawOutputChunk
	artifacts  []store.Artifact
}

func (s *runEvidenceTestStore) GetRun(_ context.Context, projectID, runID string) (store.Run, error) {
	if s.run.ProjectID != projectID || s.run.ID != runID {
		return store.Run{}, store.ErrNotFound
	}
	return s.run, nil
}

func (s *runEvidenceTestStore) GetRunProvenance(_ context.Context, projectID, runID string) (json.RawMessage, error) {
	if s.run.ProjectID != projectID || s.run.ID != runID || len(s.provenance) == 0 {
		return nil, store.ErrNotFound
	}
	return append(json.RawMessage(nil), s.provenance...), nil
}

func (s *runEvidenceTestStore) ListExecutionSessions(_ context.Context, projectID string, _ []string) ([]store.ExecutionSession, error) {
	if projectID != s.run.ProjectID {
		return nil, store.ErrNotFound
	}
	return append([]store.ExecutionSession(nil), s.sessions...), nil
}

func (s *runEvidenceTestStore) GetRuntimeInstance(_ context.Context, projectID, id string) (store.RuntimeInstance, error) {
	value, ok := s.instances[id]
	if !ok || value.ProjectID != projectID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	return value, nil
}

func (s *runEvidenceTestStore) ListRunEvents(_ context.Context, projectID, runID string, after int64, limit int) ([]store.Event, error) {
	if projectID != s.run.ProjectID || runID != s.run.ID {
		return nil, store.ErrNotFound
	}
	out := make([]store.Event, 0, limit)
	for _, event := range s.events {
		if event.Sequence == nil || *event.Sequence <= after {
			continue
		}
		out = append(out, event)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (s *runEvidenceTestStore) ListRawOutputChunks(_ context.Context, projectID, runID string) ([]store.RawOutputChunk, error) {
	if projectID != s.run.ProjectID || runID != s.run.ID {
		return nil, store.ErrNotFound
	}
	return append([]store.RawOutputChunk(nil), s.chunks...), nil
}

func (s *runEvidenceTestStore) ListArtifacts(_ context.Context, projectID, runID string) ([]store.Artifact, error) {
	if projectID != s.run.ProjectID || runID != s.run.ID {
		return nil, store.ErrNotFound
	}
	return append([]store.Artifact(nil), s.artifacts...), nil
}

func TestRunEvidenceInspectReconstructsCompleteRun(t *testing.T) {
	const projectID = "project"
	const runID = "run"
	const runtimeID = "runtime-instance"
	storeFake := &runEvidenceTestStore{
		run:        store.Run{ID: runID, ProjectID: projectID, IssueID: "issue", WorkspaceID: "workspace", Status: "READY_FOR_REVIEW"},
		provenance: json.RawMessage(`{"schemaVersion":1}`),
		sessions: []store.ExecutionSession{
			{ID: "session", ProjectID: projectID, RunID: runID, RuntimeInstanceID: runtimeID, CommandArgv: json.RawMessage(`["go","test","./..."]`)},
			{ID: "other-session", ProjectID: projectID, RunID: "other-run", RuntimeInstanceID: "other-runtime"},
		},
		instances: map[string]store.RuntimeInstance{runtimeID: {ID: runtimeID, ProjectID: projectID, RuntimeID: "runtime-config", Status: "DESTROYED"}},
		chunks:    []store.RawOutputChunk{{ID: "chunk", ProjectID: projectID, RunID: runID, Stream: "STDOUT", Sequence: 1}},
		artifacts: []store.Artifact{{ID: "artifact", ProjectID: projectID, RunID: runID, Name: "candidate.json", Kind: "candidate_manifest"}},
	}
	for i := int64(1); i <= 501; i++ {
		sequence := i
		runtime := runtimeID
		storeFake.events = append(storeFake.events, store.Event{ID: "event", ProjectID: projectID, RunID: runEvidenceStringPointer(runID), Sequence: &sequence, RuntimeInstanceID: &runtime, Type: "tool.completed"})
	}

	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Inspect(t.Context(), projectID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 501 {
		t.Fatalf("events=%d, want 501", len(got.Events))
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "session" {
		t.Fatalf("sessions=%+v", got.Sessions)
	}
	if len(got.RuntimeInstances) != 1 || got.RuntimeInstances[0].ID != runtimeID {
		t.Fatalf("runtime instances=%+v", got.RuntimeInstances)
	}
	if len(got.RawOutput) != 1 || len(got.Artifacts) != 1 {
		t.Fatalf("raw output/artifacts missing: %+v", got)
	}
	if string(got.Provenance) != `{"schemaVersion":1}` {
		t.Fatalf("provenance=%s", got.Provenance)
	}
}

func TestRunEvidenceAllowsRunBeforeProvenanceExists(t *testing.T) {
	storeFake := &runEvidenceTestStore{run: store.Run{ID: "run", ProjectID: "project"}, instances: map[string]store.RuntimeInstance{}}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Inspect(t.Context(), "project", "run")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provenance != nil {
		t.Fatalf("provenance=%s, want nil", got.Provenance)
	}
}

func TestRunEvidenceOpensOnlyContentBelongingToRequestedRun(t *testing.T) {
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	rawBlob, err := blobs.Put(t.Context(), "run", bytes.NewBufferString("stdout"))
	if err != nil {
		t.Fatal(err)
	}
	artifactBlob, err := blobs.Put(t.Context(), "run", bytes.NewBufferString("artifact"))
	if err != nil {
		t.Fatal(err)
	}
	storeFake := &runEvidenceTestStore{
		run:       store.Run{ID: "run", ProjectID: "project"},
		instances: map[string]store.RuntimeInstance{},
		chunks: []store.RawOutputChunk{{
			ID: "chunk", ProjectID: "project", RunID: "run", StorageRef: rawBlob.Ref, SizeBytes: rawBlob.SizeBytes,
		}},
		artifacts: []store.Artifact{{
			ID: "artifact", ProjectID: "project", RunID: "run", StorageRef: artifactBlob.Ref, SizeBytes: artifactBlob.SizeBytes,
		}},
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}

	_, raw, err := service.OpenRawOutput(t.Context(), "project", "run", "chunk")
	if err != nil {
		t.Fatal(err)
	}
	rawBytes, err := io.ReadAll(raw)
	_ = raw.Close()
	if err != nil || string(rawBytes) != "stdout" {
		t.Fatalf("raw content=%q err=%v", rawBytes, err)
	}

	_, artifact, err := service.OpenArtifact(t.Context(), "project", "run", "artifact")
	if err != nil {
		t.Fatal(err)
	}
	artifactBytes, err := io.ReadAll(artifact)
	_ = artifact.Close()
	if err != nil || string(artifactBytes) != "artifact" {
		t.Fatalf("artifact content=%q err=%v", artifactBytes, err)
	}

	if _, _, err := service.OpenRawOutput(t.Context(), "project", "run", "other"); err == nil {
		t.Fatal("expected cross-run/unknown raw output id to be rejected")
	} else {
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != "raw_output_not_found" {
			t.Fatalf("error=%v", err)
		}
	}
}

func runEvidenceStringPointer(value string) *string { return &value }
