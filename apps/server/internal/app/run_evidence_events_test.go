package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRequireRunValidatesAndIsolatesProjects(t *testing.T) {
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(&runEvidenceTestStore{
		run: store.Run{ID: "run", ProjectID: "project"},
	}, blobs)
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.RequireRun(context.Background(), "project", "run")
	if err != nil || got.ID != "run" {
		t.Fatalf("RequireRun() run=%+v err=%v", got, err)
	}
	if _, err := service.RequireRun(context.Background(), " ", "run"); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank project error=%v", err)
	}
	if _, err := service.RequireRun(context.Background(), "other-project", "run"); err == nil {
		t.Fatal("expected cross-project miss")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "run_not_found" {
		t.Fatalf("cross-project error=%v", err)
	}
}

func TestListEventsAfterPagesAndRejectsInvalidCursor(t *testing.T) {
	const projectID = "project"
	const runID = "run"
	storeFake := &runEvidenceTestStore{run: store.Run{ID: runID, ProjectID: projectID}}
	for i := int64(1); i <= 501; i++ {
		sequence := i
		runRef := runID
		storeFake.events = append(storeFake.events, store.Event{
			ID: "event", ProjectID: projectID, RunID: &runRef, Sequence: &sequence, Type: "agent.message",
		})
	}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}

	all, err := service.ListEventsAfter(context.Background(), projectID, runID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 501 {
		t.Fatalf("events after 0=%d want 501", len(all))
	}
	partial, err := service.ListEventsAfter(context.Background(), projectID, runID, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial) != 1 || partial[0].Sequence == nil || *partial[0].Sequence != 501 {
		t.Fatalf("events after 500=%+v", partial)
	}
	if _, err := service.ListEventsAfter(context.Background(), projectID, runID, -1); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("negative afterSequence error=%v", err)
	}
	if _, err := service.ListEventsAfter(context.Background(), "other-project", runID, 0); err == nil {
		t.Fatal("expected cross-project miss")
	}
}

func TestListEventsAfterRejectsBrokenSequencePages(t *testing.T) {
	const projectID = "project"
	const runID = "run"
	runRef := runID
	broken := &brokenSequenceEvidenceStore{run: store.Run{ID: runID, ProjectID: projectID}, runRef: &runRef}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(broken, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListEventsAfter(context.Background(), projectID, runID, 0); err == nil {
		t.Fatal("expected invalid sequence page failure")
	}
}

type brokenSequenceEvidenceStore struct {
	run    store.Run
	runRef *string
}

func (s *brokenSequenceEvidenceStore) GetRun(_ context.Context, projectID, runID string) (store.Run, error) {
	if s.run.ProjectID != projectID || s.run.ID != runID {
		return store.Run{}, store.ErrNotFound
	}
	return s.run, nil
}

func (s *brokenSequenceEvidenceStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) {
	return nil, store.ErrNotFound
}

func (s *brokenSequenceEvidenceStore) ListExecutionSessionsByRun(context.Context, string, string, []string) ([]store.ExecutionSession, error) {
	return nil, nil
}

func (s *brokenSequenceEvidenceStore) GetRuntimeInstance(context.Context, string, string) (store.RuntimeInstance, error) {
	return store.RuntimeInstance{}, store.ErrNotFound
}

func (s *brokenSequenceEvidenceStore) ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error) {
	return []store.Event{{
		ID: "broken", ProjectID: s.run.ProjectID, RunID: s.runRef, Type: "run.started",
	}}, nil
}

func (s *brokenSequenceEvidenceStore) GetRawOutputChunk(context.Context, string, string, string) (store.RawOutputChunk, error) {
	return store.RawOutputChunk{}, store.ErrNotFound
}

func (s *brokenSequenceEvidenceStore) ListRawOutputChunks(context.Context, string, string) ([]store.RawOutputChunk, error) {
	return nil, nil
}

func (s *brokenSequenceEvidenceStore) GetArtifact(context.Context, string, string, string) (store.Artifact, error) {
	return store.Artifact{}, store.ErrNotFound
}

func (s *brokenSequenceEvidenceStore) ListArtifacts(context.Context, string, string) ([]store.Artifact, error) {
	return nil, nil
}

func TestQuestionServiceSetPersistedEventPublisherNilReceiver(t *testing.T) {
	var service *QuestionService
	service.SetPersistedEventPublisher(&capturingPublisher{})
}
