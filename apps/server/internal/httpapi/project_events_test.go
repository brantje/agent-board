package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectEventStore struct {
	fakeControlPlaneStore
	events        []store.Event
	hub           *evidence.Hub
	publishOnList store.Event
}

func (s *projectEventStore) ListProjectEventsAfter(_ context.Context, pid, afterID string, limit int) ([]store.Event, error) {
	if s.hub != nil && s.publishOnList.ID != "" {
		_ = s.hub.Publish(context.Background(), s.publishOnList)
	}
	if pid != projectID {
		return nil, store.ErrNotFound
	}
	if afterID != "" {
		found := false
		var createdAt time.Time
		var id string
		for _, event := range s.events {
			if event.ID == afterID {
				found = true
				createdAt = event.CreatedAt
				id = event.ID
				break
			}
		}
		if !found {
			return nil, store.ErrNotFound
		}
		out := make([]store.Event, 0)
		for _, event := range s.events {
			if event.CreatedAt.After(createdAt) || (event.CreatedAt.Equal(createdAt) && event.ID > id) {
				out = append(out, event)
				if limit > 0 && len(out) >= limit {
					break
				}
			}
		}
		return out, nil
	}
	return nil, nil
}

func TestProjectEventStreamReplaysAfterIDThenForwardsLive(t *testing.T) {
	hub := evidence.NewHub()
	first := store.Event{
		ID: "11111111-aaaa-4aaa-8aaa-111111111111", SchemaVersion: 1, Type: "issue.created",
		ProjectID: projectID, IssueID: strPtr(issueID), Actor: store.EmptyObject, Payload: store.EmptyObject,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
	}
	second := store.Event{
		ID: "22222222-aaaa-4aaa-8aaa-222222222222", SchemaVersion: 1, Type: "issue.status_changed",
		ProjectID: projectID, IssueID: strPtr(issueID), Actor: store.EmptyObject, Payload: json.RawMessage(`{"status":"TODO"}`),
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC),
	}
	router := newProjectEventRouter(t, hub, []store.Event{first, second})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, cancel := openProjectEventStream(t, server, first.ID)
	defer cancel()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type=%q", got)
	}

	frames := readSSEFrames(t, resp.Body, 1)
	if frames[0].ID != second.ID || frames[0].Event.Type != "issue.status_changed" || frames[0].Event.IssueID == nil || *frames[0].Event.IssueID != issueID {
		t.Fatalf("replay frame=%+v", frames[0])
	}

	live := store.Event{
		ID: "33333333-aaaa-4aaa-8aaa-333333333333", SchemaVersion: 1, Type: "issue.assigned",
		ProjectID: projectID, IssueID: strPtr(issueID), Actor: store.EmptyObject, Payload: store.EmptyObject,
	}
	if err := hub.Publish(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	liveFrames := readSSEFrames(t, resp.Body, 1)
	if liveFrames[0].ID != live.ID || liveFrames[0].Event.Type != "issue.assigned" {
		t.Fatalf("live frame=%+v", liveFrames[0])
	}
}

func TestProjectEventStreamSubscribesBeforeCatchUpQuery(t *testing.T) {
	hub := evidence.NewHub()
	first := store.Event{
		ID: "11111111-aaaa-4aaa-8aaa-111111111111", SchemaVersion: 1, Type: "issue.created",
		ProjectID: projectID, IssueID: strPtr(issueID), Actor: store.EmptyObject, Payload: store.EmptyObject,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
	}
	second := store.Event{
		ID: "22222222-aaaa-4aaa-8aaa-222222222222", SchemaVersion: 1, Type: "issue.status_changed",
		ProjectID: projectID, IssueID: strPtr(issueID), Actor: store.EmptyObject, Payload: store.EmptyObject,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC),
	}
	duringCatchUp := store.Event{
		ID: "44444444-aaaa-4aaa-8aaa-444444444444", SchemaVersion: 1, Type: "issue.updated",
		ProjectID: projectID, IssueID: strPtr(issueID), Actor: store.EmptyObject, Payload: store.EmptyObject,
	}
	router := NewRouterWithApplication(&app.Services{
		ControlPlane: app.New(&projectEventStore{events: []store.Event{first, second}, hub: hub, publishOnList: duringCatchUp}),
		EventHub:     hub,
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, cancel := openProjectEventStream(t, server, first.ID)
	defer cancel()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	frames := readSSEFrames(t, resp.Body, 2)
	if frames[0].ID != second.ID {
		t.Fatalf("replay frame=%+v", frames[0])
	}
	if frames[1].ID != duringCatchUp.ID {
		t.Fatalf("missed live event published during catch-up: %+v", frames[1])
	}
}

func TestProjectEventStreamRejectsCrossProject(t *testing.T) {
	hub := evidence.NewHub()
	router := newProjectEventRouter(t, hub, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+otherID+"/events", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") == "text/event-stream" {
		t.Fatalf("isolation failure streamed: %s", rec.Body.String())
	}
}

func TestProjectEventStreamRejectsInvalidAfterID(t *testing.T) {
	hub := evidence.NewHub()
	router := newProjectEventRouter(t, hub, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/events?afterId=not-a-uuid", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProjectEventStreamRejectsUnknownAfterID(t *testing.T) {
	hub := evidence.NewHub()
	router := newProjectEventRouter(t, hub, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/events?afterId="+otherID, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProjectEventStreamHeartbeatComment(t *testing.T) {
	previous := runEventHeartbeat
	runEventHeartbeat = 20 * time.Millisecond
	t.Cleanup(func() { runEventHeartbeat = previous })

	hub := evidence.NewHub()
	router := newProjectEventRouter(t, hub, nil)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, cancel := openProjectEventStream(t, server, "")
	defer cancel()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	deadline := time.Now().Add(time.Second)
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, err := resp.Body.Read(buf)
		if n > 0 && strings.Contains(string(buf[:n]), ":") {
			return
		}
		if err != nil {
			t.Fatalf("read heartbeat: %v", err)
		}
	}
	t.Fatal("missing SSE heartbeat comment")
}

func TestProjectEventOpenAPIPath(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "api")
	mainData, err := os.ReadFile(filepath.Join(root, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainData), "/api/projects/{projectID}/events:") {
		t.Fatal("OpenAPI missing project event stream route")
	}
	pathsData, err := os.ReadFile(filepath.Join(root, "paths", "project-events.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pathsDoc := string(pathsData)
	if !strings.Contains(pathsDoc, "streamProjectEvents") || !strings.Contains(pathsDoc, "afterId") || !strings.Contains(pathsDoc, "text/event-stream:") {
		t.Fatalf("project event stream contract is missing: %s", pathsDoc)
	}
}

func newProjectEventRouter(t *testing.T, hub *evidence.Hub, events []store.Event) http.Handler {
	t.Helper()
	return NewRouterWithApplication(&app.Services{
		ControlPlane: app.New(&projectEventStore{events: events, hub: hub}),
		EventHub:     hub,
	})
}

func openProjectEventStream(t *testing.T, server *httptest.Server, afterID string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	url := server.URL + "/api/projects/" + projectID + "/events"
	if afterID != "" {
		url += "?afterId=" + afterID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return resp, cancel
}

func strPtr(value string) *string { return &value }
