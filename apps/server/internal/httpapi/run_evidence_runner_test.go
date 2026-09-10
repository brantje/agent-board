package httpapi

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestExecutionSessionEvidenceIncludesRunnerProvenance(t *testing.T) {
	dto := executionSessionEvidenceDTO(store.ExecutionSession{ID: "session-1", RunnerID: "runner-1"})
	if dto.RunnerID == nil || *dto.RunnerID != "runner-1" {
		t.Fatalf("runner id=%v", dto.RunnerID)
	}

	legacy := executionSessionEvidenceDTO(store.ExecutionSession{ID: "session-2"})
	if legacy.RunnerID != nil {
		t.Fatalf("legacy session runner id=%v", legacy.RunnerID)
	}
}
