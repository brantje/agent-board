package workspace

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCandidateApplierRollsBackFailedCandidateDelivery(t *testing.T) {
	git := requireGit(t).GitCLI
	repo := createFixtureRepository(t, git, t.TempDir())
	applier, err := NewCandidateApplier(
		candidateLockStore{},
		candidateProjectSource{workspace: store.ProjectWorkspace{ProjectID: "project-1", Path: repo}},
		git,
	)
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: "project-1"}
	sourceErr := errors.New("candidate source unavailable")

	cases := []struct {
		name      string
		reviewID  string
		candidate AcceptedCandidate
		wantErr   error
	}{
		{
			name:     "staged source failure",
			reviewID: "review-staged-source",
			candidate: AcceptedCandidate{StagedPatch: func(context.Context) (io.ReadCloser, error) {
				return nil, sourceErr
			}},
			wantErr: sourceErr,
		},
		{
			name:     "invalid staged patch",
			reviewID: "review-invalid-staged",
			candidate: AcceptedCandidate{StagedPatch: func(context.Context) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("not a patch\n")), nil
			}},
		},
		{
			name:     "unstaged source failure",
			reviewID: "review-unstaged-source",
			candidate: AcceptedCandidate{UnstagedPatch: func(context.Context) (io.ReadCloser, error) {
				return nil, sourceErr
			}},
			wantErr: sourceErr,
		},
		{
			name:     "candidate file conflicts with accepted path",
			reviewID: "review-file-conflict",
			candidate: AcceptedCandidate{Files: []CandidateFileSource{{
				Path: "README.md",
				Chunks: []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("replacement\n")), nil
				}},
			}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := applier.Apply(t.Context(), project, tc.reviewID, tc.candidate); err == nil {
				t.Fatal("Apply() unexpectedly succeeded")
			} else if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("Apply() error=%v want=%v", err, tc.wantErr)
			}

			status, err := git.run(t.Context(), "-C", repo, "status", "--porcelain")
			if err != nil {
				t.Fatal(err)
			}
			if status != "" {
				t.Fatalf("failed candidate left accepted checkout dirty: %q", status)
			}
		})
	}
}
