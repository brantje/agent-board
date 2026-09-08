package workspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type candidateErrorReader struct{ err error }

func (r candidateErrorReader) Read([]byte) (int, error) { return 0, r.err }
func (r candidateErrorReader) Close() error             { return nil }

type candidateCloseErrorReader struct {
	io.Reader
	err error
}

func (r candidateCloseErrorReader) Close() error { return r.err }

type candidateErrorWriter struct{ err error }

func (w candidateErrorWriter) Write([]byte) (int, error) { return 0, w.err }

type candidateProjectSource struct {
	workspace store.ProjectWorkspace
	err       error
}

func (s candidateProjectSource) EnsureProjectWorkspace(context.Context, store.Project) (store.ProjectWorkspace, error) {
	return s.workspace, s.err
}

type candidateLockStore struct{ err error }

func (s candidateLockStore) AcquireWorkspaceBootstrapLock(context.Context, string) (store.WorkspaceBootstrapLock, error) {
	if s.err != nil {
		return nil, s.err
	}
	return candidateLock{}, nil
}

type candidateLock struct{}

func (candidateLock) Release() error { return nil }

func TestReadCandidateBlobHandlesSourcesAndReaderErrors(t *testing.T) {
	value, err := readCandidateBlob(t.Context(), func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("patch\n")), nil
	})
	if err != nil || string(value) != "patch\n" {
		t.Fatalf("readCandidateBlob()=%q err=%v", value, err)
	}

	sourceErr := errors.New("source unavailable")
	if _, err := readCandidateBlob(t.Context(), func(context.Context) (io.ReadCloser, error) {
		return nil, sourceErr
	}); !errors.Is(err, sourceErr) {
		t.Fatalf("source error=%v", err)
	}

	readErr := errors.New("read failed")
	if _, err := readCandidateBlob(t.Context(), func(context.Context) (io.ReadCloser, error) {
		return candidateErrorReader{err: readErr}, nil
	}); !errors.Is(err, readErr) {
		t.Fatalf("reader error=%v", err)
	}
}

func TestCopyCandidateChunksHandlesFailureBoundaries(t *testing.T) {
	if err := copyCandidateChunks(t.Context(), io.Discard, []CandidateBlobSource{nil}); err == nil {
		t.Fatal("nil chunk source should fail")
	}

	sourceErr := errors.New("source failed")
	if err := copyCandidateChunks(t.Context(), io.Discard, []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
		return nil, sourceErr
	}}); !errors.Is(err, sourceErr) {
		t.Fatalf("source error=%v", err)
	}

	writeErr := errors.New("write failed")
	if err := copyCandidateChunks(t.Context(), candidateErrorWriter{err: writeErr}, []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("content")), nil
	}}); !errors.Is(err, writeErr) {
		t.Fatalf("write error=%v", err)
	}

	readErr := errors.New("read failed")
	if err := copyCandidateChunks(t.Context(), io.Discard, []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
		return candidateErrorReader{err: readErr}, nil
	}}); !errors.Is(err, readErr) {
		t.Fatalf("read error=%v", err)
	}

	closeErr := errors.New("close failed")
	if err := copyCandidateChunks(t.Context(), io.Discard, []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
		return candidateCloseErrorReader{Reader: strings.NewReader("content"), err: closeErr}, nil
	}}); !errors.Is(err, closeErr) {
		t.Fatalf("close error=%v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := copyCandidateChunks(ctx, io.Discard, []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("content")), nil
	}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}

	var output bytes.Buffer
	if err := copyCandidateChunks(t.Context(), &output, []CandidateBlobSource{
		func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("one")), nil },
		func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("two")), nil },
	}); err != nil || output.String() != "onetwo" {
		t.Fatalf("output=%q err=%v", output.String(), err)
	}
}

func TestAcceptedCandidatePathRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"", ".", "..", filepath.Join("..", "escape"), filepath.Join(root, "absolute")} {
		if _, err := acceptedCandidatePath(root, relative); err == nil {
			t.Fatalf("path %q should fail", relative)
		}
	}

	parentFile := filepath.Join(root, "file-parent")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := acceptedCandidatePath(root, filepath.Join("file-parent", "child")); err == nil {
		t.Fatal("non-directory parent should fail")
	}

	if runtime.GOOS != "windows" {
		target := t.TempDir()
		if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := acceptedCandidatePath(root, filepath.Join("link", "child")); err == nil {
			t.Fatal("symbolic-link traversal should fail")
		}
	}

	path, err := acceptedCandidatePath(root, filepath.Join("safe", "nested.txt"))
	if err != nil || path != filepath.Join(root, "safe", "nested.txt") {
		t.Fatalf("safe path=%q err=%v", path, err)
	}
}

func TestWriteCandidateFilesCreatesNestedContent(t *testing.T) {
	root := t.TempDir()
	files := []CandidateFileSource{
		{Path: filepath.Join("z", "two.txt"), Chunks: []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("two")), nil
		}}},
		{Path: filepath.Join("a", "one.txt"), Chunks: []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("one")), nil
		}}},
	}
	if err := writeCandidateFiles(t.Context(), root, files); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{filepath.Join("a", "one.txt"): "one", filepath.Join("z", "two.txt"): "two"} {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(content) != want {
			t.Fatalf("%s=%q err=%v", path, content, err)
		}
	}
	if err := writeCandidateFiles(t.Context(), root, []CandidateFileSource{{Path: "../escape"}}); err == nil {
		t.Fatal("escaping path should fail")
	}
}

func TestCandidateApplierValidatesDependenciesAndEarlyFailures(t *testing.T) {
	git := requireGit(t).GitCLI
	projectSource := candidateProjectSource{workspace: store.ProjectWorkspace{Path: t.TempDir()}}
	lockStore := candidateLockStore{}
	for name, dependencies := range map[string]struct {
		locks    ProjectWorkspaceLockStore
		projects ProjectWorkspaceSource
		git      candidateGit
	}{
		"locks":    {projects: projectSource, git: git},
		"projects": {locks: lockStore, git: git},
		"git":      {locks: lockStore, projects: projectSource},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCandidateApplier(dependencies.locks, dependencies.projects, dependencies.git); err == nil {
				t.Fatal("missing dependency should fail")
			}
		})
	}

	applier, err := NewCandidateApplier(lockStore, projectSource, git)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applier.Apply(t.Context(), store.Project{ID: "project"}, " ", AcceptedCandidate{}); err == nil {
		t.Fatal("blank review id should fail")
	}

	projectErr := errors.New("project unavailable")
	applier, _ = NewCandidateApplier(lockStore, candidateProjectSource{err: projectErr}, git)
	if _, err := applier.Apply(t.Context(), store.Project{ID: "project"}, "review", AcceptedCandidate{}); !errors.Is(err, projectErr) {
		t.Fatalf("project error=%v", err)
	}

	lockErr := errors.New("lock unavailable")
	applier, _ = NewCandidateApplier(candidateLockStore{err: lockErr}, projectSource, git)
	if _, err := applier.Apply(t.Context(), store.Project{ID: "project"}, "review", AcceptedCandidate{}); !errors.Is(err, lockErr) {
		t.Fatalf("lock error=%v", err)
	}
}
