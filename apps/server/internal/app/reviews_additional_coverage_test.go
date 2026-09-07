package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestReviewServiceListAndDecisionProjection(t *testing.T) {
	run := store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"}
	decisionID := "decision-1"
	review := store.Review{ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: run.ID, Status: "APPROVED", DecisionID: &decisionID}
	s := &reviewServiceStore{
		run:      run,
		review:   review,
		list:     []store.Review{review},
		decision: store.Decision{ID: decisionID, Kind: "REVIEW", Outcome: "APPROVED"},
	}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	values, err := service.List(context.Background(), "project-1", store.ReviewFilter{Statuses: []string{"APPROVED"}})
	if err != nil || len(values) != 1 || values[0].ID != review.ID {
		t.Fatalf("List()=%+v err=%v", values, err)
	}
	inspection, err := service.Get(context.Background(), "project-1", review.ID)
	if err != nil || inspection.Decision == nil || inspection.Decision.ID != decisionID {
		t.Fatalf("Get()=%+v err=%v", inspection, err)
	}

	s.decision.Kind = "REVIEW_APPROVAL_INTENT"
	inspection, err = service.Get(context.Background(), "project-1", review.ID)
	if err != nil || inspection.Decision != nil {
		t.Fatalf("lifecycle decision leaked: %+v err=%v", inspection, err)
	}
}

func TestReviewServiceValidatesCommandIdentifiers(t *testing.T) {
	service := newReviewServiceForTest(t, &reviewServiceStore{}, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	if _, err := service.List(context.Background(), " ", store.ReviewFilter{}); err == nil {
		t.Fatal("List should reject blank project")
	}
	if _, err := service.Get(context.Background(), "", "review"); err == nil {
		t.Fatal("Get should reject blank project")
	}
	if _, err := service.Get(context.Background(), "project", " "); err == nil {
		t.Fatal("Get should reject blank review")
	}
	if _, err := service.Approve(context.Background(), "", "review", nil); err == nil {
		t.Fatal("Approve should reject blank project")
	}
	if _, err := service.Approve(context.Background(), "project", "", nil); err == nil {
		t.Fatal("Approve should reject blank review")
	}
	if _, err := service.RequestChanges(context.Background(), "", "review", "fix", nil); err == nil {
		t.Fatal("RequestChanges should reject blank project")
	}
	if _, err := service.RequestChanges(context.Background(), "project", "", "fix", nil); err == nil {
		t.Fatal("RequestChanges should reject blank review")
	}
}

func TestReviewServiceApprovalFailuresRemainRecoverable(t *testing.T) {
	run := store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"}
	review := store.Review{ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: run.ID, Status: "PENDING"}
	manifest := store.Artifact{ID: "manifest", ProjectID: "project-1", RunID: run.ID, Kind: "candidate_manifest", StorageRef: "manifest"}
	blobs := &reviewBlobStore{values: map[string][]byte{"manifest": []byte(`{}`)}}

	t.Run("begin persistence failure", func(t *testing.T) {
		s := &reviewServiceStore{run: run, review: review, beginErr: store.ErrConflict}
		service := newReviewServiceForTest(t, s, blobs, &reviewCandidateApplierFake{})
		if _, err := service.Approve(context.Background(), "project-1", review.ID, nil); err == nil {
			t.Fatal("expected begin failure")
		}
	})

	t.Run("apply failure records retryable failure", func(t *testing.T) {
		s := &reviewServiceStore{
			project:   store.Project{ID: "project-1"},
			run:       run,
			review:    review,
			begin:     store.BeginReviewApprovalResult{Review: review, Run: run},
			artifacts: []store.Artifact{manifest},
		}
		service := newReviewServiceForTest(t, s, blobs, &reviewCandidateApplierFake{err: errors.New("apply failed")})
		if _, err := service.Approve(context.Background(), "project-1", review.ID, nil); err == nil {
			t.Fatal("expected apply failure")
		}
		if s.failCalls != 1 {
			t.Fatalf("failure persistence calls=%d", s.failCalls)
		}
	})

	t.Run("post-apply completion failure leaves intent recoverable", func(t *testing.T) {
		s := &reviewServiceStore{
			project:     store.Project{ID: "project-1"},
			run:         run,
			review:      review,
			begin:       store.BeginReviewApprovalResult{Review: review, Run: run},
			artifacts:   []store.Artifact{manifest},
			completeErr: store.ErrConflict,
		}
		service := newReviewServiceForTest(t, s, blobs, &reviewCandidateApplierFake{revision: "accepted"})
		if _, err := service.Approve(context.Background(), "project-1", review.ID, nil); err == nil {
			t.Fatal("expected completion failure")
		}
		if s.failCalls != 0 {
			t.Fatalf("post-apply failure must not fail approval intent: calls=%d", s.failCalls)
		}
	})
}

func TestReviewServiceRequestChangesTranslatesStoreFailure(t *testing.T) {
	s := &reviewServiceStore{requestErr: store.ErrConflict}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	if _, err := service.RequestChanges(context.Background(), "project", "review", "fix", nil); err == nil {
		t.Fatal("expected store failure")
	}
}

func TestAcceptedCandidateReconstructionValidatesAndOrdersChunks(t *testing.T) {
	s := &reviewServiceStore{}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	base := RunEvidence{Run: store.Run{ID: "run", ProjectID: "project"}}
	manifest := store.Artifact{ID: "manifest", Kind: "candidate_manifest"}

	t.Run("requires exactly one manifest", func(t *testing.T) {
		if _, err := service.acceptedCandidate(base); err == nil {
			t.Fatal("missing manifest should fail")
		}
		value := base
		value.Artifacts = []store.Artifact{manifest, {ID: "manifest-2", Kind: "candidate_manifest"}}
		if _, err := service.acceptedCandidate(value); err == nil {
			t.Fatal("duplicate manifest should fail")
		}
	})

	t.Run("rejects duplicate patches", func(t *testing.T) {
		value := base
		value.Artifacts = []store.Artifact{
			manifest,
			{ID: "p1", Kind: "candidate_patch", Name: "candidate-staged.patch"},
			{ID: "p2", Kind: "candidate_patch", Name: "candidate-staged.patch"},
		}
		if _, err := service.acceptedCandidate(value); err == nil {
			t.Fatal("duplicate staged patch should fail")
		}
		value.Artifacts = []store.Artifact{
			manifest,
			{ID: "p1", Kind: "candidate_patch", Name: "candidate-unstaged.patch"},
			{ID: "p2", Kind: "candidate_patch", Name: "candidate-unstaged.patch"},
		}
		if _, err := service.acceptedCandidate(value); err == nil {
			t.Fatal("duplicate unstaged patch should fail")
		}
	})

	t.Run("validates file metadata", func(t *testing.T) {
		for name, metadata := range map[string]json.RawMessage{
			"malformed":   json.RawMessage(`{`),
			"missing path": json.RawMessage(`{"chunkIndex":0,"chunkCount":1}`),
			"bad index":    json.RawMessage(`{"path":"a.txt","chunkIndex":2,"chunkCount":1}`),
		} {
			t.Run(name, func(t *testing.T) {
				value := base
				value.Artifacts = []store.Artifact{manifest, {ID: "file", Kind: "candidate_file_chunk", SafeMetadata: metadata}}
				if _, err := service.acceptedCandidate(value); err == nil {
					t.Fatal("invalid metadata should fail")
				}
			})
		}
	})

	t.Run("sorts files and chunk sources", func(t *testing.T) {
		chunk := func(path string, index, count int) store.Artifact {
			metadata, _ := json.Marshal(map[string]any{"path": path, "chunkIndex": index, "chunkCount": count})
			return store.Artifact{ID: path + string(rune('0'+index)), Kind: "candidate_file_chunk", SafeMetadata: metadata}
		}
		value := base
		value.Artifacts = []store.Artifact{manifest, chunk("z.txt", 1, 2), chunk("a.txt", 0, 1), chunk("z.txt", 0, 2)}
		candidate, err := service.acceptedCandidate(value)
		if err != nil {
			t.Fatal(err)
		}
		if len(candidate.Files) != 2 || candidate.Files[0].Path != "a.txt" || candidate.Files[1].Path != "z.txt" || len(candidate.Files[1].Chunks) != 2 {
			t.Fatalf("candidate files=%+v", candidate.Files)
		}
	})

	t.Run("rejects inconsistent and incomplete chunks", func(t *testing.T) {
		metadata1, _ := json.Marshal(map[string]any{"path": "a.txt", "chunkIndex": 0, "chunkCount": 2})
		metadata2, _ := json.Marshal(map[string]any{"path": "a.txt", "chunkIndex": 1, "chunkCount": 3})
		value := base
		value.Artifacts = []store.Artifact{manifest, {ID: "c1", Kind: "candidate_file_chunk", SafeMetadata: metadata1}, {ID: "c2", Kind: "candidate_file_chunk", SafeMetadata: metadata2}}
		if _, err := service.acceptedCandidate(value); err == nil {
			t.Fatal("changed chunk count should fail")
		}
		value.Artifacts = []store.Artifact{manifest, {ID: "c1", Kind: "candidate_file_chunk", SafeMetadata: metadata1}}
		if _, err := service.acceptedCandidate(value); err == nil {
			t.Fatal("incomplete chunks should fail")
		}
	})
}
