package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAcceptedCandidatePreservesExecutableMetadata(t *testing.T) {
	metadata := func(index int, executable bool) json.RawMessage {
		value, err := json.Marshal(map[string]any{
			"path":       "bin/run.sh",
			"executable": executable,
			"chunkIndex": index,
			"chunkCount": 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	evidence := RunEvidence{
		Run: store.Run{ID: "run-1", ProjectID: "project-1"},
		Artifacts: []store.Artifact{
			{ID: "manifest", Kind: "candidate_manifest"},
			{ID: "chunk-0", Kind: "candidate_file_chunk", SafeMetadata: metadata(0, true)},
			{ID: "chunk-1", Kind: "candidate_file_chunk", SafeMetadata: metadata(1, true)},
		},
	}
	candidate, err := (&ReviewService{}).acceptedCandidate(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.Files) != 1 || candidate.Files[0].Path != "bin/run.sh" || !candidate.Files[0].Executable {
		t.Fatalf("candidate files=%+v", candidate.Files)
	}
}

func TestAcceptedCandidateRejectsMixedExecutableChunkMetadata(t *testing.T) {
	metadata := func(index int, executable bool) json.RawMessage {
		value, err := json.Marshal(map[string]any{
			"path":       "bin/run.sh",
			"executable": executable,
			"chunkIndex": index,
			"chunkCount": 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	_, err := (&ReviewService{}).acceptedCandidate(RunEvidence{
		Run: store.Run{ID: "run-1", ProjectID: "project-1"},
		Artifacts: []store.Artifact{
			{ID: "manifest", Kind: "candidate_manifest"},
			{ID: "chunk-0", Kind: "candidate_file_chunk", SafeMetadata: metadata(0, true)},
			{ID: "chunk-1", Kind: "candidate_file_chunk", SafeMetadata: metadata(1, false)},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "executable metadata changed") {
		t.Fatalf("error=%v, want executable metadata conflict", err)
	}
}