package workspace

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCandidateFilesRejectsExistingProjectWorkspacePath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.txt")
	if err := os.WriteFile(path, []byte("accepted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := writeCandidateFiles(context.Background(), root, []CandidateFileSource{{
		Path: "new.txt",
		Chunks: []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("candidate\n")), nil
		}},
	}})
	if err == nil {
		t.Fatal("expected collision to reject candidate file")
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "accepted\n" {
		t.Fatalf("existing accepted content changed to %q", content)
	}
}
