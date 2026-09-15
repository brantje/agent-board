package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type blockingIssuePatchStore struct {
	*Store
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingIssuePatchStore) UpdateIssuePatchMutation(ctx context.Context, patch store.IssuePatch, actor json.RawMessage) (store.IssueMutationResult, error) {
	if patch.Title != nil {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return s.Store.UpdateIssuePatchMutation(ctx, patch, actor)
}

func TestIssuePatchPreservesConcurrentExplicitStatusAndDoesNotEnqueue(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "patch-concurrency")
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "original", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='AGENT',assignee_id=$2 WHERE id=$1`, issue.ID, f.agent.ID); err != nil {
		t.Fatal(err)
	}

	blocking := &blockingIssuePatchStore{Store: s, entered: make(chan struct{}), release: make(chan struct{})}
	router := httpapi.NewRouter(app.New(blocking))
	path := "/api/projects/" + f.project.ID + "/issues/" + issue.Key

	titleResult := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(`{"title":"renamed"}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		titleResult <- rec
	}()
	<-blocking.entered

	statusReq := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(`{"status":"BACKLOG"}`))
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status PATCH code=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	close(blocking.release)
	titleRec := <-titleResult
	if titleRec.Code != http.StatusOK {
		t.Fatalf("title PATCH code=%d body=%s", titleRec.Code, titleRec.Body.String())
	}

	got, err := s.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "renamed" || got.Status != "BACKLOG" {
		t.Fatalf("Issue after interleaving=%+v", got)
	}
	var runs int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE project_id=$1 AND issue_id=$2`, f.project.ID, issue.ID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("unexpected Runs=%d", runs)
	}
	rows, err := s.pool.Query(ctx, `SELECT type FROM events WHERE project_id=$1 AND issue_id=$2 ORDER BY created_at,id`, f.project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatal(err)
		}
		types = append(types, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"issue.created", "issue.status_changed", "issue.updated"}
	if len(types) != len(want) {
		t.Fatalf("events=%v want=%v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events=%v want=%v", types, want)
		}
	}
}
