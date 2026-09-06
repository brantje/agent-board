package app

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunEvidenceValidatesScopeAndContentIdentifiers(t *testing.T) {
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunEvidenceService(nil, blobs); err == nil {
		t.Fatal("expected nil store to be rejected")
	}
	storeFake := &runEvidenceTestStore{
		run:       store.Run{ID: "run", ProjectID: "project"},
		instances: map[string]store.RuntimeInstance{},
	}
	if _, err := NewRunEvidenceService(storeFake, nil); err == nil {
		t.Fatal("expected nil blob store to be rejected")
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Inspect(t.Context(), "", "run"); err == nil {
		t.Fatal("expected empty project id to be rejected")
	}
	if _, err := service.Inspect(t.Context(), "project", "missing"); err == nil {
		t.Fatal("expected missing run to be rejected")
	}
	if _, _, err := service.OpenRawOutput(t.Context(), "project", "run", ""); err == nil {
		t.Fatal("expected empty raw output id to be rejected")
	}
	if _, _, err := service.OpenArtifact(t.Context(), "project", "run", ""); err == nil {
		t.Fatal("expected empty artifact id to be rejected")
	}
	if _, _, err := service.OpenArtifact(t.Context(), "project", "run", "missing"); err == nil {
		t.Fatal("expected missing artifact to be rejected")
	} else {
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != "artifact_not_found" {
			t.Fatalf("missing artifact error=%v", err)
		}
	}
}

func TestRunEvidenceReportsMissingRuntimeAndBlobContent(t *testing.T) {
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	storeFake := &runEvidenceTestStore{
		run: store.Run{ID: "run", ProjectID: "project"},
		sessions: []store.ExecutionSession{{
			ID: "session", ProjectID: "project", RunID: "run", RuntimeInstanceID: "missing-runtime",
		}},
		instances: map[string]store.RuntimeInstance{},
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Inspect(t.Context(), "project", "run"); err == nil {
		t.Fatal("expected missing runtime instance to fail inspection")
	}

	storeFake.sessions = nil
	storeFake.chunks = []store.RawOutputChunk{{ID: "chunk", ProjectID: "project", RunID: "run", StorageRef: "not-a-blob"}}
	if _, _, err := service.OpenRawOutput(t.Context(), "project", "run", "chunk"); err == nil {
		t.Fatal("expected invalid raw output storage reference to fail")
	}
	storeFake.artifacts = []store.Artifact{{ID: "artifact", ProjectID: "project", RunID: "run", StorageRef: "not-a-blob"}}
	if _, _, err := service.OpenArtifact(t.Context(), "project", "run", "artifact"); err == nil {
		t.Fatal("expected invalid artifact storage reference to fail")
	}
}
