package evidence

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestCandidateSnapshotRetryUsesPinnedArchiveWithoutWorkspaceAccess(t *testing.T) {
	workspace := candidateSnapshotRepository(t)
	writeFile(t, workspace, "tracked.txt", "reviewed change\n")

	blobs, err := NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := NewReviewCandidateStore(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	scope := RunScope{ProjectID: "project", IssueID: "issue", RunID: "run-pinned-retry"}
	firstSnapshotter, err := NewCandidateSnapshotterWithReviewCandidates(NewCandidateCollector(), &candidateArtifactMemoryStore{}, blobs, archive)
	if err != nil {
		t.Fatal(err)
	}
	first, err := firstSnapshotter.Snapshot(t.Context(), scope, workspace)
	if err != nil {
		t.Fatal(err)
	}

	secondSnapshotter, err := NewCandidateSnapshotterWithReviewCandidates(NewCandidateCollector(), &candidateArtifactMemoryStore{}, blobs, archive)
	if err != nil {
		t.Fatal(err)
	}
	missingWorkspace := filepath.Join(t.TempDir(), "workspace-no-longer-available")
	second, err := secondSnapshotter.Snapshot(t.Context(), scope, missingWorkspace)
	if err != nil {
		t.Fatalf("retry should use pinned candidate without Workspace access: %v", err)
	}
	if !reflect.DeepEqual(first.Candidate, second.Candidate) {
		t.Fatalf("retry candidate changed: first=%+v second=%+v", first.Candidate, second.Candidate)
	}
}
