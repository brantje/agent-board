package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
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
	workspace := candidateSnapshotRepository(t)

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

func TestCandidateSnapshotChunksPostRedactionContent(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	writeFile(t, workspace, "redacted-untracked.txt", strings.Repeat("x", 100))

	base, err := NewFileBlobStore(t.TempDir(), 128)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := NewRedactingBlobStore(base, replacingRedactor{old: "x", replacement: "***"})
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

	var chunks []store.Artifact
	for _, artifact := range snapshot.Artifacts {
		if artifact.Kind == "candidate_file_chunk" {
			chunks = append(chunks, artifact)
		}
	}
	if len(chunks) != 3 {
		t.Fatalf("candidate chunks=%d, want 3 post-redaction chunks", len(chunks))
	}

	want := []byte(strings.Repeat("***", 100))
	var restored bytes.Buffer
	for index, artifact := range chunks {
		var metadata struct {
			ChunkIndex int   `json:"chunkIndex"`
			ChunkCount int   `json:"chunkCount"`
			Offset     int64 `json:"offset"`
		}
		if err := json.Unmarshal(artifact.SafeMetadata, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.ChunkIndex != index || metadata.ChunkCount != len(chunks) || metadata.Offset != int64(index*128) {
			t.Fatalf("chunk %d metadata=%+v", index, metadata)
		}
		reader, err := base.Open(t.Context(), artifact.StorageRef)
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
		t.Fatalf("restored redacted candidate=%d bytes, want %d", restored.Len(), len(want))
	}
	if bytes.Contains(restored.Bytes(), []byte("x")) {
		t.Fatal("raw secret remained in persisted candidate chunks")
	}
}

func TestCandidateSnapshotRejectsUntrackedSymbolicLink(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "outside-link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	blobs, err := NewFileBlobStore(t.TempDir(), 128)
	if err != nil {
		t.Fatal(err)
	}
	snapshotter, err := NewCandidateSnapshotter(NewCandidateCollector(), &candidateArtifactMemoryStore{}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshotter.Snapshot(t.Context(), RunScope{ProjectID: "project", IssueID: "issue", RunID: "run"}, workspace)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("snapshot error=%v, want symbolic-link rejection", err)
	}
}

func TestCandidateSnapshotRejectsUnboundedChunkCount(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	path := filepath.Join(workspace, "huge-untracked.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((maxCandidateFileChunks + 1) * 128); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
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
	_, err = snapshotter.Snapshot(t.Context(), RunScope{ProjectID: "project", IssueID: "issue", RunID: "run"}, workspace)
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("snapshot error=%v, want bounded chunk rejection", err)
	}
}

func TestOpenCandidateRegularFileSafetyBoundaries(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "regular.txt"), []byte("safe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}

	file, info, err := openCandidateRegularFile(workspace, "regular.txt")
	if err != nil {
		t.Fatalf("open regular candidate: %v", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		t.Fatalf("opened mode=%s, want regular file", info.Mode())
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for name, relative := range map[string]string{
		"missing":   "missing.txt",
		"directory": "directory",
		"escape":    "../outside.txt",
	} {
		t.Run(name, func(t *testing.T) {
			file, _, err := openCandidateRegularFile(workspace, relative)
			if file != nil {
				_ = file.Close()
			}
			if err == nil {
				t.Fatalf("openCandidateRegularFile(%q) succeeded, want rejection", relative)
			}
		})
	}
}

func TestCandidateChunkCountValidation(t *testing.T) {
	for name, input := range map[string]struct {
		size  int64
		limit int64
	}{
		"negative size":  {size: -1, limit: 128},
		"zero limit":     {size: 1, limit: 0},
		"negative limit": {size: 1, limit: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := candidateChunkCount(input.size, input.limit); err == nil {
				t.Fatalf("candidateChunkCount(%d, %d) succeeded, want error", input.size, input.limit)
			}
		})
	}
}

func TestCandidateChunkCountIsOverflowSafe(t *testing.T) {
	if _, err := candidateChunkCount(math.MaxInt64, 128); err == nil {
		t.Fatal("expected extreme candidate size to be rejected")
	}
	count, err := candidateChunkCount(257, 128)
	if err != nil || count != 3 {
		t.Fatalf("chunk count=%d err=%v", count, err)
	}
}

func candidateSnapshotRepository(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	writeFile(t, workspace, "tracked.txt", "base\n")
	runGit(t, workspace, "add", "tracked.txt")
	runGit(t, workspace, "commit", "-m", "base")
	return workspace
}
