package evidence

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type candidateArtifactMemoryStore struct {
	artifacts []store.Artifact
}

func (s *candidateArtifactMemoryStore) CreateArtifact(_ context.Context, artifact store.Artifact) (store.Artifact, error) {
	artifact.ID = fmt.Sprintf("artifact-%d", len(s.artifacts)+1)
	s.artifacts = append(s.artifacts, artifact)
	return artifact, nil
}

func TestCandidateSnapshotChunksOversizedUntrackedFile(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	writeFile(t, workspace, "tracked.txt", "base\n")
	runGit(t, workspace, "add", "tracked.txt")
	runGit(t, workspace, "commit", "-m", "base")

	want := []byte(strings.Repeat("oversized-candidate-", 32))
	writeFile(t, workspace, "large-untracked.txt", string(want))
	blobs, err := NewFileBlobStore(t.TempDir(), 128)
	if err != nil {
		t.Fatal(err)
	}
	artifactStore := &candidateArtifactMemoryStore{}
	snapshotter, err := NewCandidateSnapshotter(NewCandidateCollector(), artifactStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotter.Snapshot(t.Context(), RunScope{ProjectID: "project", IssueID: "issue", RunID: "run"}, workspace)
	if err != nil {
		t.Fatal(err)
	}

	var chunks []store.Artifact
	for _, artifact := range snapshot.Artifacts {
		if artifact.Kind == "candidate_file_chunk" {
			chunks = append(chunks, artifact)
		}
	}
	if len(chunks) < 2 {
		t.Fatalf("candidate chunks=%d, want multiple artifacts", len(chunks))
	}
	var restored bytes.Buffer
	for _, artifact := range chunks {
		reader, err := blobs.Open(t.Context(), artifact.StorageRef)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(&restored, reader); err != nil {
			_ = reader.Close()
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(restored.Bytes(), want) {
		t.Fatalf("restored oversized candidate differs: got=%d bytes want=%d", restored.Len(), len(want))
	}
}
