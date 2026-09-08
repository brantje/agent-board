package workspace

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteCandidateFilesPreservesExecutableIntent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable permission bits are not portable to Windows")
	}
	root := t.TempDir()
	source := func(value string) CandidateBlobSource {
		return func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(value)), nil
		}
	}
	if err := writeCandidateFiles(t.Context(), root, []CandidateFileSource{
		{Path: "bin/run.sh", Chunks: []CandidateBlobSource{source("#!/bin/sh\n")}, Executable: true},
		{Path: "README.txt", Chunks: []CandidateBlobSource{source("plain\n")}},
	}); err != nil {
		t.Fatal(err)
	}

	executable, err := os.Stat(filepath.Join(root, "bin", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if executable.Mode().Perm()&0o111 == 0 {
		t.Fatalf("run.sh mode=%#o, want executable", executable.Mode().Perm())
	}
	plain, err := os.Stat(filepath.Join(root, "README.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Mode().Perm()&0o111 != 0 {
		t.Fatalf("README.txt mode=%#o, want non-executable", plain.Mode().Perm())
	}
}
