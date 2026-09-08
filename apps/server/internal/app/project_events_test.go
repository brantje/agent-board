package app

import (
	"context"
	"strconv"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectEventListStore struct {
	fakeStore
	events []store.Event
	calls  int
}

func (s *projectEventListStore) ListProjectEventsAfter(_ context.Context, projectID, afterID string, limit int) ([]store.Event, error) {
	s.calls++
	if projectID != s.project.ID {
		return nil, store.ErrNotFound
	}
	found := false
	start := -1
	for i, event := range s.events {
		if event.ID == afterID {
			found = true
			start = i + 1
			break
		}
	}
	if !found {
		return nil, store.ErrNotFound
	}
	if start >= len(s.events) {
		return nil, nil
	}
	out := append([]store.Event(nil), s.events[start:]...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func TestListProjectEventsAfterPagesAndIsolatesProjects(t *testing.T) {
	const projectID = "project"
	events := make([]store.Event, 0, projectEventPageSize+2)
	for i := 0; i < projectEventPageSize+2; i++ {
		events = append(events, store.Event{ID: "e" + strconv.Itoa(i), ProjectID: projectID, Type: "issue.updated"})
	}
	base := &projectEventListStore{
		fakeStore: fakeStore{project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}},
		events:    events,
	}
	svc := New(base)

	all, err := svc.ListProjectEventsAfter(context.Background(), projectID, events[0].ID)
	if err != nil || len(all) != projectEventPageSize+1 {
		t.Fatalf("len=%d err=%v", len(all), err)
	}
	if base.calls < 2 {
		t.Fatalf("expected paging calls, got %d", base.calls)
	}

	empty, err := svc.ListProjectEventsAfter(context.Background(), projectID, "")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty afterId=%d err=%v", len(empty), err)
	}

	if _, err := svc.ListProjectEventsAfter(context.Background(), "missing", events[0].ID); err == nil {
		t.Fatal("expected project isolation")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "project_not_found" {
		t.Fatalf("isolation error=%v", err)
	}

	if _, err := svc.ListProjectEventsAfter(context.Background(), projectID, "missing-event"); err == nil {
		t.Fatal("expected missing afterId")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "event_not_found" {
		t.Fatalf("afterId error=%v", err)
	}
}

func TestListProjectEventsAfterCapsReplayPages(t *testing.T) {
	previousSize, previousMax := projectEventPageSize, projectEventMaxPages
	projectEventPageSize, projectEventMaxPages = 2, 2
	t.Cleanup(func() {
		projectEventPageSize, projectEventMaxPages = previousSize, previousMax
	})

	const projectID = "project"
	events := make([]store.Event, 0, 10)
	for i := 0; i < 10; i++ {
		events = append(events, store.Event{ID: "e" + strconv.Itoa(i), ProjectID: projectID, Type: "issue.updated"})
	}
	base := &projectEventListStore{
		fakeStore: fakeStore{project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}},
		events:    events,
	}
	got, err := New(base).ListProjectEventsAfter(context.Background(), projectID, events[0].ID)
	if err != nil || len(got) != 4 {
		t.Fatalf("len=%d err=%v", len(got), err)
	}
	if base.calls != 2 {
		t.Fatalf("calls=%d", base.calls)
	}
}
