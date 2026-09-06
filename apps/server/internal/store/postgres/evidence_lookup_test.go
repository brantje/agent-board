package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunScopedEvidenceGettersEnforceScope(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	fixture := seedRunFixture(t, s, "run-evidence-getters")
	other := seedRunFixture(t, s, "run-evidence-getters-other")

	chunk, err := s.CreateRawOutputChunk(ctx, store.RawOutputChunk{
		ProjectID: fixture.project.ID,
		IssueID: fixture.issue.ID,
		RunID: fixture.run.ID,
		Stream: "STDOUT",
		Sequence: 1,
		StorageRef: "blob:raw-output",
		SizeBytes: 6,
	})
	if err != nil {
		t.Fatalf("create raw output chunk: %v", err)
	}
	gotChunk, err := s.GetRawOutputChunk(ctx, fixture.project.ID, fixture.run.ID, chunk.ID)
	if err != nil || gotChunk.ID != chunk.ID {
		t.Fatalf("get raw output chunk=%+v err=%v", gotChunk, err)
	}
	if _, err := s.GetRawOutputChunk(ctx, other.project.ID, fixture.run.ID, chunk.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project raw output error=%v, want ErrNotFound", err)
	}

	artifact, err := s.CreateArtifact(ctx, store.Artifact{
		ProjectID: fixture.project.ID,
		IssueID: fixture.issue.ID,
		RunID: fixture.run.ID,
		Name: "candidate.json",
		Kind: "candidate_manifest",
		StorageRef: "blob:artifact",
		SizeBytes: 2,
	})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	gotArtifact, err := s.GetArtifact(ctx, fixture.project.ID, fixture.run.ID, artifact.ID)
	if err != nil || gotArtifact.ID != artifact.ID {
		t.Fatalf("get artifact=%+v err=%v", gotArtifact, err)
	}
	if _, err := s.GetArtifact(ctx, other.project.ID, fixture.run.ID, artifact.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project artifact error=%v, want ErrNotFound", err)
	}
}
