package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type releasingProcessTestSessions struct {
	processTestSessions
	released []string
}

func (s *releasingProcessTestSessions) ReleaseRunRedaction(runID string) {
	s.released = append(s.released, runID)
}

func TestProcessorReleasesRunRedactionLeaseOnExit(t *testing.T) {
	evidenceStore := &processTestStore{}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(evidenceStore, blobs, 64)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := engine.NewRegistry(processTestEngine{})
	if err != nil {
		t.Fatal(err)
	}
	sessions := &releasingProcessTestSessions{}
	processor, err := NewProcessor(
		evidenceStore,
		processTestResolver{err: errors.New("resolver unavailable")},
		&processTestRuntime{},
		sessions,
		registry,
		recorder,
		output,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	run := store.Run{ID: "run", ProjectID: "project", IssueID: "issue", WorkspaceID: "workspace", Status: "STARTING"}
	result, err := processor.Process(context.Background(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "FAILED" {
		t.Fatalf("result=%+v", result)
	}
	if len(sessions.released) != 1 || sessions.released[0] != run.ID {
		t.Fatalf("released Run redactions=%v, want [%s]", sessions.released, run.ID)
	}
}

var _ runRedactionReleaser = (*releasingProcessTestSessions)(nil)
var _ ContextResolver = processTestResolver{resolved: executioncontext.Resolved{}}
