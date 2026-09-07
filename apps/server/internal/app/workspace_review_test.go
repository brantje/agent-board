package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	workspacepkg "github.com/brantje/agent-board/apps/server/internal/workspace"
)

type reviewDeliveryMaterializer struct {
	revision string
	err      error
	reviewID string
}

func (m *reviewDeliveryMaterializer) Ensure(context.Context, store.Project, store.Issue, store.Workspace) (store.Workspace, error) {
	return store.Workspace{}, nil
}
func (m *reviewDeliveryMaterializer) ApplyReviewedCandidate(_ context.Context, _ store.Project, reviewID string, _ workspacepkg.AcceptedCandidate) (string, error) {
	m.reviewID = reviewID
	return m.revision, m.err
}

func TestWorkspaceServiceReviewDeliveryBoundary(t *testing.T) {
	lookup := &workspaceLookupFake{}
	materializer := &reviewDeliveryMaterializer{revision: "accepted-sha"}
	service, err := NewWorkspaceService(lookup, materializer)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := service.ApplyReviewedCandidate(t.Context(), store.Project{ID: "project"}, "review-1", workspacepkg.AcceptedCandidate{})
	if err != nil || revision != "accepted-sha" || materializer.reviewID != "review-1" {
		t.Fatalf("revision=%q reviewID=%q err=%v", revision, materializer.reviewID, err)
	}

	materializer.err = errors.New("conflict")
	if _, err := service.ApplyReviewedCandidate(t.Context(), store.Project{ID: "project"}, "review-2", workspacepkg.AcceptedCandidate{}); err == nil {
		t.Fatal("expected apply error")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "review_apply_failed" {
		t.Fatalf("error=%v", err)
	}
}

func TestWorkspaceServiceReviewDeliveryRejectsUnavailableMaterializer(t *testing.T) {
	assertInternalUnavailable := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("review delivery should fail")
		}
		if _, ok := AsError(err); ok {
			t.Fatalf("review delivery capability errors must remain internal: %v", err)
		}
		if err.Error() != "workspace review delivery is unavailable" {
			t.Fatalf("error=%q", err)
		}
	}

	var nilService *WorkspaceService
	_, err := nilService.ApplyReviewedCandidate(t.Context(), store.Project{}, "review", workspacepkg.AcceptedCandidate{})
	assertInternalUnavailable(t, err)

	service, err := NewWorkspaceService(&workspaceLookupFake{}, workspaceMaterializerFunc(func(context.Context, store.Project, store.Issue, store.Workspace) (store.Workspace, error) {
		return store.Workspace{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyReviewedCandidate(t.Context(), store.Project{}, "review", workspacepkg.AcceptedCandidate{})
	assertInternalUnavailable(t, err)
}
