package evidence

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type recordingArtifactStore struct {
	artifacts []store.Artifact
}

func (s *recordingArtifactStore) CreateArtifact(_ context.Context, artifact store.Artifact) (store.Artifact, error) {
	artifact.ID = fmt.Sprintf("artifact-%d", len(s.artifacts)+1)
	s.artifacts = append(s.artifacts, artifact)
	return artifact, nil
}

func TestCandidateSnapshotterPersistsImmutableCandidateEvidence(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")

	writeFile(t, root, "tracked.txt", "staged\n")
	runGit(t, root, "add", "tracked.txt")
	writeFile(t, root, "tracked.txt", "staged plus unstaged\n")
	writeFile(t, root, "new.txt", "immutable new\n")

	blobStore, err := NewFileBlobStore(filepath.Join(t.TempDir(), "blobs"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := &recordingArtifactStore{}
	snapshotter, err := NewCandidateSnapshotter(NewCandidateCollector(), artifacts, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := snapshotter.Snapshot(context.Background(), RunScope{ProjectID: "project", IssueID: "issue", RunID: "run"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Manifest.Kind != "candidate_manifest" {
		t.Fatalf("manifest kind = %q", snapshot.Manifest.Kind)
	}
	if len(snapshot.Artifacts) != 3 {
		t.Fatalf("candidate artifacts = %d, want staged patch, unstaged patch and untracked file", len(snapshot.Artifacts))
	}

	var untracked store.Artifact
	for _, artifact := range snapshot.Artifacts {
		if artifact.Kind == "candidate_file" && artifact.Name == "new.txt" {
			untracked = artifact
		}
	}
	if untracked.ID == "" {
		t.Fatalf("untracked artifact not found: %+v", snapshot.Artifacts)
	}
	before := readCandidateSnapshotBlob(t, blobStore, untracked.StorageRef)
	writeFile(t, root, "new.txt", "later mutation\n")
	after := readCandidateSnapshotBlob(t, blobStore, untracked.StorageRef)
	if string(before) != "immutable new\n" || string(after) != string(before) {
		t.Fatalf("snapshot blob changed: before=%q after=%q", before, after)
	}
}

func TestCandidateSnapshotterValidationAndPathContainment(t *testing.T) {
	if _, err := NewCandidateSnapshotter(nil, &recordingArtifactStore{}, nil); err == nil {
		t.Fatal("NewCandidateSnapshotter() accepted missing dependencies")
	}
	root := t.TempDir()
	if _, err := candidateFilePath(root, "../escape"); err == nil {
		t.Fatal("candidateFilePath() accepted workspace escape")
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := candidateFilePath(root, "sub/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "sub", "file.txt") {
		t.Fatalf("candidateFilePath() = %q", path)
	}
}

func readCandidateSnapshotBlob(t *testing.T, blobs BlobStore, ref string) []byte {
	t.Helper()
	reader, err := blobs.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
