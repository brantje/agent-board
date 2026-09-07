package app

import (
	"context"
	"io"
	"strings"
	"testing"

	evidencepkg "github.com/brantje/agent-board/apps/server/internal/evidence"
)

func TestAcceptedCandidateFromPrivatePreservesExecutableMetadata(t *testing.T) {
	candidate := acceptedCandidateFromPrivate(evidencepkg.ReviewCandidateSnapshot{
		Files: []evidencepkg.ReviewCandidateFile{{
			Path:       "bin/run.sh",
			Executable: true,
			Source: func(context.Context) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("#!/bin/sh\n")), nil
			},
		}},
	})
	if len(candidate.Files) != 1 || candidate.Files[0].Path != "bin/run.sh" || !candidate.Files[0].Executable {
		t.Fatalf("candidate files=%+v", candidate.Files)
	}
	if len(candidate.Files[0].Chunks) != 1 {
		t.Fatalf("candidate chunks=%d, want 1", len(candidate.Files[0].Chunks))
	}
}
