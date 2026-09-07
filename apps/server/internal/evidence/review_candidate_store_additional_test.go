package evidence

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewCandidateStoreCapturesStagedPatchAndEmptyCandidate(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	writeFile(t, workspace, "tracked.txt", "staged-change\n")
	command := exec.Command("git", "add", "tracked.txt")
	command.Dir = workspace
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}

	archive, err := NewReviewCandidateStore(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewCandidateCollector().Collect(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Capture(t.Context(), "run-staged", workspace, candidate); err != nil {
		t.Fatal(err)
	}
	snapshot, err := archive.Open(t.Context(), "run-staged")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.StagedPatch == nil || snapshot.UnstagedPatch != nil {
		t.Fatalf("patch sources staged=%v unstaged=%v", snapshot.StagedPatch != nil, snapshot.UnstagedPatch != nil)
	}
	if patch := readReviewCandidateSource(t, snapshot.StagedPatch); !bytes.Contains(patch, []byte("staged-change")) {
		t.Fatalf("staged patch=%q", patch)
	}

	cleanWorkspace := candidateSnapshotRepository(t)
	if err := archive.Capture(t.Context(), "run-empty", cleanWorkspace, Candidate{}); err != nil {
		t.Fatal(err)
	}
	empty, err := archive.Open(t.Context(), "run-empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Candidate.Changes) != 0 || empty.StagedPatch != nil || empty.UnstagedPatch != nil || len(empty.Files) != 0 {
		t.Fatalf("empty snapshot=%+v", empty)
	}
}

func TestReviewCandidateStoreRejectsInvalidExistingAndWorkspaceState(t *testing.T) {
	archive, err := NewReviewCandidateStore(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	badRun, err := archive.runPath("bad-run")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(badRun, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badRun, "manifest.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := archive.Capture(t.Context(), "bad-run", t.TempDir(), Candidate{}); err == nil || !strings.Contains(err.Error(), "validate existing") {
		t.Fatalf("invalid existing archive error=%v", err)
	}

	if err := archive.Capture(t.Context(), "bad-workspace", t.TempDir(), Candidate{}); err == nil || !strings.Contains(err.Error(), "git diff") {
		t.Fatalf("non-repository workspace error=%v", err)
	}
	var nilStore *ReviewCandidateStore
	if err := nilStore.Capture(t.Context(), "run", t.TempDir(), Candidate{}); err == nil {
		t.Fatal("nil store Capture should fail")
	}
	if _, err := nilStore.Open(t.Context(), "run"); err == nil {
		t.Fatal("nil store Open should fail")
	}
}

func TestReviewCandidateStoreOpenRejectsMalformedAndMissingStorage(t *testing.T) {
	archive, err := NewReviewCandidateStore(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}

	writeManifest := func(t *testing.T, runID, raw string) string {
		t.Helper()
		runPath, err := archive.runPath(runID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(runPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runPath, "manifest.json"), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		return runPath
	}

	writeManifest(t, "malformed", "{")
	if _, err := archive.Open(t.Context(), "malformed"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("malformed manifest error=%v", err)
	}
	writeManifest(t, "future", `{"version":2}`)
	if _, err := archive.Open(t.Context(), "future"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("future manifest error=%v", err)
	}
	writeManifest(t, "missing-staged", `{"version":1,"stagedPatch":"staged.patch","stagedPatchSize":1}`)
	if _, err := archive.Open(t.Context(), "missing-staged"); err == nil || !strings.Contains(err.Error(), "inspect review candidate storage") {
		t.Fatalf("missing staged patch error=%v", err)
	}
	writeManifest(t, "missing-unstaged", `{"version":1,"unstagedPatch":"unstaged.patch","unstagedPatchSize":1}`)
	if _, err := archive.Open(t.Context(), "missing-unstaged"); err == nil || !strings.Contains(err.Error(), "inspect review candidate storage") {
		t.Fatalf("missing unstaged patch error=%v", err)
	}
	writeManifest(t, "missing-file", `{"version":1,"candidate":{"changes":[{"path":"new.txt","untracked":true}]},"files":[{"path":"new.txt","storage":"files/000000","sizeBytes":1}]}`)
	if _, err := archive.Open(t.Context(), "missing-file"); err == nil || !strings.Contains(err.Error(), "inspect review candidate storage") {
		t.Fatalf("missing file error=%v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := archive.Open(ctx, "missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Open error=%v", err)
	}
}

func TestReviewCandidateStoreHelpersEnforcePrivateFileBoundary(t *testing.T) {
	root := t.TempDir()
	privateFile := filepath.Join(root, "private")
	if err := writePrivateCandidateFile(privateFile, []byte("value")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(privateFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private file mode=%v err=%v", modeOrZero(info), err)
	}
	if err := writePrivateCandidateFile(filepath.Join(root, "missing", "value"), []byte("x")); err == nil {
		t.Fatal("write into missing parent should fail")
	}

	copiedPath := filepath.Join(root, "copied")
	written, err := copyPrivateCandidate(t.Context(), copiedPath, strings.NewReader("copied-value"))
	if err != nil || written != int64(len("copied-value")) {
		t.Fatalf("copy written=%d err=%v", written, err)
	}
	if data, err := os.ReadFile(copiedPath); err != nil || string(data) != "copied-value" {
		t.Fatalf("copied data=%q err=%v", data, err)
	}
	if _, err := copyPrivateCandidate(t.Context(), copiedPath, strings.NewReader("again")); err == nil {
		t.Fatal("copy must not overwrite an existing archive file")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := copyPrivateCandidate(cancelled, filepath.Join(root, "cancelled"), strings.NewReader("data")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled copy error=%v", err)
	}
}

func TestReviewCandidateSourceOpenLifecycleAndValidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "payload")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := reviewCandidateSource(root, "payload", int64(len("payload")))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := source(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(data) != "payload" {
		t.Fatalf("source data=%q readErr=%v closeErr=%v", data, readErr, closeErr)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := source(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled source error=%v", err)
	}
	if _, err := reviewCandidateSource(root, "missing", 0); err == nil {
		t.Fatal("missing storage should fail")
	}
	if _, err := reviewCandidateSource(root, "payload", 1); err == nil || !strings.Contains(err.Error(), "size changed") {
		t.Fatalf("size mismatch error=%v", err)
	}
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewCandidateSource(root, "directory", 0); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory storage error=%v", err)
	}
}

func TestReviewCandidateStoreConstructorAndRunPathValidation(t *testing.T) {
	for _, root := range []string{"", "relative"} {
		if _, err := NewReviewCandidateStore(root); err == nil {
			t.Fatalf("root %q should fail", root)
		}
	}
	parent := t.TempDir()
	fileRoot := filepath.Join(parent, "file")
	if err := os.WriteFile(fileRoot, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReviewCandidateStore(filepath.Join(fileRoot, "child")); err == nil {
		t.Fatal("root below a regular file should fail")
	}
	archive, err := NewReviewCandidateStore(filepath.Join(parent, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{"", ".", "..", "a/b", `a\\b`} {
		if _, err := archive.runPath(runID); err == nil {
			t.Fatalf("run id %q should fail", runID)
		}
	}
	if path, err := archive.runPath("run-ok"); err != nil || filepath.Base(path) != "run-ok" {
		t.Fatalf("valid run path=%q err=%v", path, err)
	}
}
