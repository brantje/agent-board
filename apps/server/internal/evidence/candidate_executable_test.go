package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCandidateSnapshotPreservesUntrackedExecutableIntent(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	path := filepath.Join(workspace, "run.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho reviewed\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	blobs, err := NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := &candidateArtifactMemoryStore{}
	snapshotter, err := NewCandidateSnapshotter(NewCandidateCollector(), artifacts, blobs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotter.Snapshot(t.Context(), RunScope{ProjectID: "project", IssueID: "issue", RunID: "run"}, workspace)
	if err != nil {
		t.Fatal(err)
	}

	var candidateFile *store.Artifact
	for index := range snapshot.Artifacts {
		if snapshot.Artifacts[index].Kind == "candidate_file" && snapshot.Artifacts[index].Name == "run.sh" {
			candidateFile = &snapshot.Artifacts[index]
			break
		}
	}
	if candidateFile == nil {
		t.Fatal("executable candidate file artifact was not created")
	}
	var metadata struct {
		Path       string `json:"path"`
		Executable bool   `json:"executable"`
	}
	if err := json.Unmarshal(candidateFile.SafeMetadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Path != "run.sh" || !metadata.Executable {
		t.Fatalf("candidate metadata=%+v", metadata)
	}
}

func TestCandidateSnapshotRepeatsExecutableIntentOnEveryChunk(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	path := filepath.Join(workspace, "large-run.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+string(make([]byte, 512))), 0o755); err != nil {
		t.Fatal(err)
	}

	blobs, err := NewFileBlobStore(t.TempDir(), 128)
	if err != nil {
		t.Fatal(err)
	}
	snapshotter, err := NewCandidateSnapshotter(NewCandidateCollector(), &candidateArtifactMemoryStore{}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotter.Snapshot(t.Context(), RunScope{ProjectID: "project", IssueID: "issue", RunID: "run"}, workspace)
	if err != nil {
		t.Fatal(err)
	}

	chunks := 0
	for _, artifact := range snapshot.Artifacts {
		if artifact.Kind != "candidate_file_chunk" {
			continue
		}
		chunks++
		var metadata struct {
			Executable bool `json:"executable"`
		}
		if err := json.Unmarshal(artifact.SafeMetadata, &metadata); err != nil {
			t.Fatal(err)
		}
		if !metadata.Executable {
			t.Fatalf("chunk %q lost executable intent", artifact.Name)
		}
	}
	if chunks < 2 {
		t.Fatalf("candidate chunks=%d, want multiple", chunks)
	}
}
