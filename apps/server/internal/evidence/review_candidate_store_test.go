package evidence

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
)

func TestPrivateReviewCandidatePinsExactBytesWhilePublicEvidenceIsRedacted(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	const (
		runID  = "run-review-private"
		secret = "super-secret-review-token"
	)
	writeFile(t, workspace, "tracked.txt", "tracked="+secret+"\n")
	writeFile(t, workspace, "bin/run.sh", "#!/bin/sh\necho "+secret+"\n")
	if err := os.Chmod(filepath.Join(workspace, "bin/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	registry := redaction.NewRegistry()
	registry.Register(runID, []string{secret})
	defer registry.Release(runID)
	baseBlobs, err := NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	publicBlobs, err := NewRedactingBlobStore(baseBlobs, registry)
	if err != nil {
		t.Fatal(err)
	}
	archiveRoot := filepath.Join(t.TempDir(), "review-candidates")
	archive, err := NewReviewCandidateStore(archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := &candidateArtifactMemoryStore{}
	snapshotter, err := NewCandidateSnapshotterWithReviewCandidates(NewCandidateCollector(), artifacts, publicBlobs, archive)
	if err != nil {
		t.Fatal(err)
	}
	scope := RunScope{ProjectID: "project", IssueID: "issue", RunID: runID}
	first, err := snapshotter.Snapshot(t.Context(), scope, workspace)
	if err != nil {
		t.Fatal(err)
	}

	private, err := archive.Open(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	patch := readReviewCandidateSource(t, private.UnstagedPatch)
	if !bytes.Contains(patch, []byte(secret)) {
		t.Fatalf("private patch lost exact candidate bytes: %q", patch)
	}
	if len(private.Files) != 1 || private.Files[0].Path != "bin/run.sh" || !private.Files[0].Executable {
		t.Fatalf("private files=%+v", private.Files)
	}
	fileBytes := readReviewCandidateSource(t, private.Files[0].Source)
	if !bytes.Contains(fileBytes, []byte(secret)) {
		t.Fatalf("private candidate file lost exact bytes: %q", fileBytes)
	}

	assertPublicCandidateRedacted(t, baseBlobs, append([]storeArtifactForTest{{artifact: first.Manifest}}, artifactsForTest(first.Artifacts)...), secret)

	// A retry for the same Run must keep projecting the already-pinned candidate,
	// even if the mutable Issue Workspace changed after the first snapshot.
	writeFile(t, workspace, "tracked.txt", "changed-after-capture\n")
	writeFile(t, workspace, "bin/run.sh", "changed-after-capture\n")
	secondArtifacts := &candidateArtifactMemoryStore{}
	secondSnapshotter, err := NewCandidateSnapshotterWithReviewCandidates(NewCandidateCollector(), secondArtifacts, publicBlobs, archive)
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondSnapshotter.Snapshot(t.Context(), scope, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Candidate, second.Candidate) {
		t.Fatalf("retry candidate changed: first=%+v second=%+v", first.Candidate, second.Candidate)
	}
	for _, artifact := range append([]storeArtifactForTest{{artifact: second.Manifest}}, artifactsForTest(second.Artifacts)...) {
		data := readPublicArtifact(t, baseBlobs, artifact.artifact.StorageRef)
		if bytes.Contains(data, []byte("changed-after-capture")) {
			t.Fatalf("retry public evidence reread mutable Workspace: %q", data)
		}
	}

	runPath, err := archive.runPath(runID)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(runPath); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("archive directory mode=%v err=%v", modeOrZero(info), err)
	}
	if info, err := os.Stat(filepath.Join(runPath, "manifest.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive manifest mode=%v err=%v", modeOrZero(info), err)
	}
}

type storeArtifactForTest struct {
	artifact struct {
		StorageRef string
	}
}

func artifactsForTest(values []store.Artifact) []storeArtifactForTest {
	result := make([]storeArtifactForTest, 0, len(values))
	for _, artifact := range values {
		result = append(result, storeArtifactForTest{artifact: struct{ StorageRef string }{StorageRef: artifact.StorageRef}})
	}
	return result
}

func assertPublicCandidateRedacted(t *testing.T, blobs BlobStore, artifacts []storeArtifactForTest, secret string) {
	t.Helper()
	for _, value := range artifacts {
		data := readPublicArtifact(t, blobs, value.artifact.StorageRef)
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("public candidate evidence leaked secret: %q", data)
		}
	}
}

func readPublicArtifact(t *testing.T, blobs BlobStore, ref string) []byte {
	t.Helper()
	reader, err := blobs.Open(t.Context(), ref)
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

func readReviewCandidateSource(t *testing.T, source ReviewCandidateBlobSource) []byte {
	t.Helper()
	if source == nil {
		t.Fatal("review candidate source is nil")
	}
	reader, err := source(t.Context())
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

func modeOrZero(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}

func TestReviewCandidateStoreValidationAndRecoveryBoundaries(t *testing.T) {
	if _, err := NewReviewCandidateStore("relative/path"); err == nil {
		t.Fatal("relative archive root should fail")
	}
	archive, err := NewReviewCandidateStore(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Open(t.Context(), "missing"); !errors.Is(err, ErrReviewCandidateNotFound) {
		t.Fatalf("missing archive error=%v", err)
	}
	if err := archive.Capture(t.Context(), "../escape", t.TempDir(), Candidate{}); err == nil {
		t.Fatal("invalid run id should fail")
	}

	for name, manifest := range map[string]reviewCandidateManifest{
		"staged metadata": {
			Version:         reviewCandidateManifestVersion,
			StagedPatchSize: 1,
		},
		"unstaged metadata": {
			Version:           reviewCandidateManifestVersion,
			UnstagedPatchSize: 1,
		},
		"negative patch size": {
			Version:         reviewCandidateManifestVersion,
			StagedPatch:     "staged.patch",
			StagedPatchSize: -1,
		},
		"missing change path": {
			Version:   reviewCandidateManifestVersion,
			Candidate: Candidate{Changes: []CandidateChange{{Untracked: true}}},
		},
		"duplicate change path": {
			Version: reviewCandidateManifestVersion,
			Candidate: Candidate{Changes: []CandidateChange{
				{Path: "a.txt"},
				{Path: "a.txt"},
			}},
		},
		"file not untracked": {
			Version:   reviewCandidateManifestVersion,
			Candidate: Candidate{Changes: []CandidateChange{{Path: "a.txt"}}},
			Files:     []reviewCandidateManifestFile{{Path: "a.txt", Storage: "files/000000"}},
		},
		"incomplete files": {
			Version:   reviewCandidateManifestVersion,
			Candidate: Candidate{Changes: []CandidateChange{{Path: "a.txt", Untracked: true}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateReviewCandidateManifest(manifest); err == nil {
				t.Fatal("invalid manifest should fail")
			}
		})
	}
}

func TestReviewCandidateStoreReusesFirstCaptureAndDetectsSizeTampering(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	writeFile(t, workspace, "new.txt", "first\n")
	archive, err := NewReviewCandidateStore(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewCandidateCollector().Collect(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Capture(t.Context(), "run-1", workspace, candidate); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, "new.txt", "second\n")
	if err := archive.Capture(t.Context(), "run-1", workspace, Candidate{}); err != nil {
		t.Fatalf("existing valid archive should be reusable: %v", err)
	}
	opened, err := archive.Open(t.Context(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(readReviewCandidateSource(t, opened.Files[0].Source)); got != "first\n" {
		t.Fatalf("reused archive content=%q, want first capture", got)
	}

	runPath, err := archive.runPath("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runPath, "files", "000000"), []byte("tampered-size"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Open(t.Context(), "run-1"); err == nil || !strings.Contains(err.Error(), "size changed") {
		t.Fatalf("tampered archive error=%v", err)
	}
}

func TestReviewCandidateSourceRejectsEscapesAndSymlinks(t *testing.T) {
	root := t.TempDir()
	if _, err := reviewCandidateSource(root, "../outside", 0); err == nil {
		t.Fatal("storage escape should fail")
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := reviewCandidateSource(root, "link", 4); err == nil {
		t.Fatal("symlink storage should fail")
	}
}

var _ = context.Background
