package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

type reviewEvidenceStore struct {
	runEvidenceTestStore
	review    store.Review
	workspace store.Workspace
}

func (s *reviewEvidenceStore) GetReviewByRun(_ context.Context, projectID, runID string) (store.Review, error) {
	if projectID != s.run.ProjectID || runID != s.run.ID {
		return store.Review{}, store.ErrNotFound
	}
	return s.review, nil
}

func (s *reviewEvidenceStore) GetWorkspace(_ context.Context, projectID, workspaceID string) (store.Workspace, error) {
	if projectID != s.run.ProjectID || workspaceID != s.run.WorkspaceID {
		return store.Workspace{}, store.ErrNotFound
	}
	return s.workspace, nil
}

type fakeReviewGit struct {
	changes []workspace.ReviewFileChange
}

func (f *fakeReviewGit) ReviewFileChanges(context.Context, string, string, string) ([]workspace.ReviewFileChange, error) {
	return f.changes, nil
}

func TestRunEvidenceInspectDerivesReviewFileChangesFromGit(t *testing.T) {
	const projectID = "project"
	const runID = "run"
	storeFake := &reviewEvidenceStore{
		runEvidenceTestStore: runEvidenceTestStore{
			run: store.Run{ID: runID, ProjectID: projectID, IssueID: "issue", WorkspaceID: "workspace", Status: "READY_FOR_REVIEW"},
		},
		review: store.Review{
			ProjectID: projectID, RunID: runID, BaseRevision: "base", ReviewRevision: "review",
		},
		workspace: store.Workspace{ID: "workspace", ProjectID: projectID, IssueID: "issue", Path: "/repo"},
	}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	service.ConfigureReviewFileChanges(storeFake, &fakeReviewGit{
		changes: []workspace.ReviewFileChange{
			{Path: "index.html", ChangeType: "modified", Added: 899, Removed: 499},
		},
	})

	got, err := service.Inspect(t.Context(), projectID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events=%+v", got.Events)
	}
	event := got.Events[0]
	if event.Type != "file.modified" {
		t.Fatalf("event type=%q", event.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["path"] != "index.html" || payload["added"] != float64(899) || payload["removed"] != float64(499) || payload["source"] != "git" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestRunEvidenceInspectSkipsGitDerivationWhenFileEventsExist(t *testing.T) {
	const projectID = "project"
	const runID = "run"
	sequence := int64(1)
	storeFake := &reviewEvidenceStore{
		runEvidenceTestStore: runEvidenceTestStore{
			run:    store.Run{ID: runID, ProjectID: projectID, IssueID: "issue", WorkspaceID: "workspace", Status: "READY_FOR_REVIEW"},
			events: []store.Event{{ID: "file-1", Type: "file.modified", ProjectID: projectID, RunID: runEvidenceStringPointer(runID), Sequence: &sequence, Payload: json.RawMessage(`{"path":"README.md"}`)}},
		},
		review: store.Review{ProjectID: projectID, RunID: runID, BaseRevision: "base", ReviewRevision: "review"},
	}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	service.ConfigureReviewFileChanges(storeFake, &fakeReviewGit{changes: []workspace.ReviewFileChange{{Path: "index.html", ChangeType: "modified", Added: 1, Removed: 1}}})

	got, err := service.Inspect(t.Context(), projectID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || got.Events[0].ID != "file-1" {
		t.Fatalf("events=%+v", got.Events)
	}
}
