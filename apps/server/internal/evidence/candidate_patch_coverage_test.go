package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateSnapshotPersistsStagedAndUnstagedPatches(t *testing.T) {
	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	writeFile(t, workspace, "staged.txt", "base\n")
	writeFile(t, workspace, "unstaged.txt", "base\n")
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "-m", "base")

	if err := os.WriteFile(filepath.Join(workspace, "staged.txt"), []byte("staged change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, workspace, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(workspace, "unstaged.txt"), []byte("unstaged change\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	blobs, err := NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	artifactStore := &candidateArtifactMemoryStore{}
	snapshotter, err := NewCandidateSnapshotter(NewCandidateCollector(), artifactStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotter.Snapshot(t.Context(), RunScope{ProjectID: "project", IssueID: "issue", RunID: "run"}, workspace)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, artifact := range snapshot.Artifacts {
		if artifact.Kind == "candidate_patch" {
			seen[artifact.Name] = true
		}
	}
	for _, name := range []string{"candidate-staged.patch", "candidate-unstaged.patch"} {
		if !seen[name] {
			t.Fatalf("missing %s in %+v", name, snapshot.Artifacts)
		}
	}
}
