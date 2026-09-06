package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *runEvidenceTestStore) GetRawOutputChunk(_ context.Context, projectID, runID, chunkID string) (store.RawOutputChunk, error) {
	if projectID != s.run.ProjectID || runID != s.run.ID {
		return store.RawOutputChunk{}, store.ErrNotFound
	}
	for _, chunk := range s.chunks {
		if chunk.ID == chunkID {
			return chunk, nil
		}
	}
	return store.RawOutputChunk{}, store.ErrNotFound
}

func (s *runEvidenceTestStore) GetArtifact(_ context.Context, projectID, runID, artifactID string) (store.Artifact, error) {
	if projectID != s.run.ProjectID || runID != s.run.ID {
		return store.Artifact{}, store.ErrNotFound
	}
	for _, artifact := range s.artifacts {
		if artifact.ID == artifactID {
			return artifact, nil
		}
	}
	return store.Artifact{}, store.ErrNotFound
}

type noListRunEvidenceStore struct {
	*runEvidenceTestStore
}

func (s *noListRunEvidenceStore) ListRawOutputChunks(context.Context, string, string) ([]store.RawOutputChunk, error) {
	return nil, errors.New("unexpected run-wide raw output scan")
}

func (s *noListRunEvidenceStore) ListArtifacts(context.Context, string, string) ([]store.Artifact, error) {
	return nil, errors.New("unexpected run-wide artifact scan")
}

func TestRunEvidenceContentLookupAvoidsRunWideScans(t *testing.T) {
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

	base := &runEvidenceTestStore{
		run:       store.Run{ID: "run", ProjectID: "project"},
		instances: map[string]store.RuntimeInstance{},
		chunks: []store.RawOutputChunk{{
			ID: "chunk", ProjectID: "project", RunID: "run", StorageRef: rawBlob.Ref, SizeBytes: rawBlob.SizeBytes,
		}},
		artifacts: []store.Artifact{{
			ID: "artifact", ProjectID: "project", RunID: "run", StorageRef: artifactBlob.Ref, SizeBytes: artifactBlob.SizeBytes,
		}},
	}
	service, err := NewRunEvidenceService(&noListRunEvidenceStore{runEvidenceTestStore: base}, blobs)
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
}
