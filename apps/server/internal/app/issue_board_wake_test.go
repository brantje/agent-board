package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueBoardWakeStore struct {
	*fakeStore
	result store.IssueMutationResult
	err    error
}

func (s *issueBoardWakeStore) PlaceIssue(context.Context, store.IssueBoardPlacement) (store.IssueMutationResult, error) {
	return s.result, s.err
}

type countingBoardWaker struct{ calls int }

func (w *countingBoardWaker) Wake() { w.calls++ }

func TestPlaceIssueWakesSchedulerAfterSuccessfulPlacement(t *testing.T) {
	project := store.Project{ID: "project"}
	board := &issueBoardWakeStore{
		fakeStore: &fakeStore{project: project},
		result: store.IssueMutationResult{Issue: store.Issue{ID: "issue", ProjectID: project.ID, Status: "TODO"}},
	}
	service := New(board)
	waker := &countingBoardWaker{}
	service.SetIssueBoardSchedulerWaker(waker)

	if _, err := service.PlaceIssue(context.Background(), store.IssueBoardPlacement{ProjectID: project.ID, IssueID: "issue", Status: "TODO"}); err != nil {
		t.Fatalf("PlaceIssue() error=%v", err)
	}
	if waker.calls != 1 {
		t.Fatalf("Wake() calls=%d want=1", waker.calls)
	}

	board.err = errors.New("placement failed")
	if _, err := service.PlaceIssue(context.Background(), store.IssueBoardPlacement{ProjectID: project.ID, IssueID: "issue", Status: "TODO"}); err == nil {
		t.Fatal("PlaceIssue() expected error")
	}
	if waker.calls != 1 {
		t.Fatalf("failed placement woke scheduler: calls=%d", waker.calls)
	}
}
