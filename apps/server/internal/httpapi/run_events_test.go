package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type sseEvidenceStore struct {
	httpRunEvidenceStore
	events []store.Event
}

func (s *sseEvidenceStore) ListRunEvents(_ context.Context, pid, id string, after int64, limit int) ([]store.Event, error) {
	if pid != projectID || id != runID {
		return nil, store.ErrNotFound
	}
	out := make([]store.Event, 0)
	for _, event := range s.events {
		if event.Sequence == nil || *event.Sequence <= after {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func TestRunEventStreamReplaysAfterSequenceThenForwardsLive(t *testing.T) {
	hub := evidence.NewHub()
	one, two, three := int64(1), int64(2), int64(3)
	runRef := runID
	storeEvents := []store.Event{
		{ID: "11111111-aaaa-4aaa-8aaa-111111111111", SchemaVersion: 1, Type: "run.started", ProjectID: projectID, RunID: &runRef, Sequence: &one, Actor: store.EmptyObject, Payload: store.EmptyObject},
		{ID: "22222222-aaaa-4aaa-8aaa-222222222222", SchemaVersion: 1, Type: "agent.message", ProjectID: projectID, RunID: &runRef, Sequence: &two, Actor: store.EmptyObject, Payload: json.RawMessage(`{"kind":"message"}`)},
	}
	router, blobs := newRunEventRouter(t, hub, storeEvents)
	_ = blobs
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, cancel := openRunEventStream(t, server, "1")
	defer cancel()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type=%q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("cache-control=%q", got)
	}

	frames := readSSEFrames(t, resp.Body, 1)
	if frames[0].ID != storeEvents[1].ID || frames[0].Event.Type != "agent.message" || frames[0].Event.Sequence == nil || *frames[0].Event.Sequence != 2 {
		t.Fatalf("replay frame=%+v", frames[0])
	}

	live := store.Event{
		ID: "33333333-aaaa-4aaa-8aaa-333333333333", SchemaVersion: 1, Type: "question.answered",
		ProjectID: projectID, RunID: &runRef, Sequence: &three, Actor: store.EmptyObject, Payload: json.RawMessage(`{"questionId":"` + otherID + `"}`),
	}
	if err := hub.Publish(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	liveFrames := readSSEFrames(t, resp.Body, 1)
	if liveFrames[0].ID != live.ID || liveFrames[0].Event.Type != "question.answered" {
		t.Fatalf("live frame=%+v", liveFrames[0])
	}
}

func TestRunEventStreamDedupesOverlappingLiveIDs(t *testing.T) {
	hub := evidence.NewHub()
	one := int64(1)
	runRef := runID
	persisted := store.Event{
		ID: "11111111-aaaa-4aaa-8aaa-111111111111", SchemaVersion: 1, Type: "run.started",
		ProjectID: projectID, RunID: &runRef, Sequence: &one, Actor: store.EmptyObject, Payload: store.EmptyObject,
	}
	router, _ := newRunEventRouter(t, hub, []store.Event{persisted})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, cancel := openRunEventStream(t, server, "0")
	defer cancel()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	first := readSSEFrames(t, resp.Body, 1)
	if first[0].ID != persisted.ID {
		t.Fatalf("catch-up=%+v", first[0])
	}
	if err := hub.Publish(context.Background(), persisted); err != nil {
		t.Fatal(err)
	}
	two := int64(2)
	next := store.Event{
		ID: "22222222-aaaa-4aaa-8aaa-222222222222", SchemaVersion: 1, Type: "agent.message",
		ProjectID: projectID, RunID: &runRef, Sequence: &two, Actor: store.EmptyObject, Payload: store.EmptyObject,
	}
	if err := hub.Publish(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	second := readSSEFrames(t, resp.Body, 1)
	if second[0].ID != next.ID {
		t.Fatalf("expected live event after duplicate, got %+v", second[0])
	}
}

func TestRunEventStreamRejectsCrossProjectRun(t *testing.T) {
	hub := evidence.NewHub()
	router, _ := newRunEventRouter(t, hub, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+otherID+"/runs/"+runID+"/events", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "text/event-stream") || rec.Header().Get("Content-Type") == "text/event-stream" {
		t.Fatalf("isolation failure streamed: %s", rec.Body.String())
	}
}

func TestRunEventStreamHeartbeatComment(t *testing.T) {
	previous := runEventHeartbeat
	runEventHeartbeat = 20 * time.Millisecond
	t.Cleanup(func() { runEventHeartbeat = previous })

	hub := evidence.NewHub()
	router, _ := newRunEventRouter(t, hub, nil)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, cancel := openRunEventStream(t, server, "")
	defer cancel()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	deadline := time.Now().Add(time.Second)
	reader := bufio.NewReader(resp.Body)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read heartbeat: %v", err)
		}
		if strings.HasPrefix(strings.TrimSpace(line), ":") {
			return
		}
	}
	t.Fatal("missing SSE heartbeat comment")
}

func TestRunEventStreamRejectsInvalidAfterSequence(t *testing.T) {
	hub := evidence.NewHub()
	router, _ := newRunEventRouter(t, hub, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/runs/"+runID+"/events?afterSequence=-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunEventStreamRequiresFlusher(t *testing.T) {
	hub := evidence.NewHub()
	router, _ := newRunEventRouter(t, hub, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/runs/"+runID+"/events", nil)
	rec := &nonFlushingResponse{header: make(http.Header)}
	router.ServeHTTP(rec, req)
	if rec.status != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.status, rec.body.String())
	}
	if !strings.Contains(rec.body.String(), "streaming is unavailable") {
		t.Fatalf("body=%s", rec.body.String())
	}
}

func TestRunEventStreamEndsWhenCatchUpFails(t *testing.T) {
	hub := evidence.NewHub()
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	evidenceService, err := app.NewRunEvidenceService(&failingListEvidenceStore{}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithApplication(&app.Services{
		ControlPlane: app.New(&fakeControlPlaneStore{}),
		RunEvidence:  evidenceService,
		EventHub:     hub,
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, cancel := openRunEventStream(t, server, "0")
	defer cancel()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "data:") {
		t.Fatalf("unexpected SSE payload after catch-up failure: %q", data)
	}
}

type nonFlushingResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *nonFlushingResponse) Header() http.Header { return r.header }
func (r *nonFlushingResponse) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(p)
}
func (r *nonFlushingResponse) WriteHeader(status int) { r.status = status }

type failingListEvidenceStore struct {
	httpRunEvidenceStore
}

func (s *failingListEvidenceStore) ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error) {
	return nil, errors.New("list events unavailable")
}

func TestQuestionAnswerAppearsOnRunEventStream(t *testing.T) {
	hub := evidence.NewHub()
	runRef := runID
	seq := int64(4)
	answered := store.Event{
		ID: "44444444-aaaa-4aaa-8aaa-444444444444", SchemaVersion: 1, Type: "question.answered",
		ProjectID: projectID, RunID: &runRef, Sequence: &seq, Actor: store.EmptyObject,
		Payload: json.RawMessage(`{"questionId":"` + otherID + `"}`),
	}
	questionStore := &publishingQuestionStore{apiQuestionStore: apiQuestionStore{questions: []store.Question{{
		ID: otherID, ProjectID: projectID, IssueID: issueID, RunID: runID,
		Prompt: "Which strategy?", Kind: "TEXT", Options: json.RawMessage(`[]`), Blocking: true, Status: "OPEN",
	}}}, events: []store.Event{answered}}
	questionService, err := app.NewQuestionService(questionStore)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(&discardingEventStore{}, hub)
	if err != nil {
		t.Fatal(err)
	}
	questionService.SetPersistedEventPublisher(recorder)

	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	evidenceService, err := app.NewRunEvidenceService(&sseEvidenceStore{}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithApplication(&app.Services{
		ControlPlane: app.New(&fakeControlPlaneStore{}),
		RunEvidence:  evidenceService,
		EventHub:     hub,
		Questions:    questionService,
		Events:       recorder,
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, cancel := openRunEventStream(t, server, "0")
	defer cancel()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	answer := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/questions/"+otherID+"/answer", strings.NewReader(`{"kind":"TEXT","text":"Use the safe path"}`))
	answer.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, answer)
	if rec.Code != http.StatusOK {
		t.Fatalf("answer status=%d body=%s", rec.Code, rec.Body.String())
	}

	frames := readSSEFrames(t, resp.Body, 1)
	if frames[0].ID != answered.ID || frames[0].Event.Type != "question.answered" {
		t.Fatalf("answered frame=%+v", frames[0])
	}
}

func newRunEventRouter(t *testing.T, hub *evidence.Hub, events []store.Event) (http.Handler, *evidence.FileBlobStore) {
	t.Helper()
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	evidenceService, err := app.NewRunEvidenceService(&sseEvidenceStore{events: events}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	return NewRouterWithApplication(&app.Services{
		ControlPlane: app.New(&fakeControlPlaneStore{}),
		RunEvidence:  evidenceService,
		EventHub:     hub,
	}), blobs
}

func openRunEventStream(t *testing.T, server *httptest.Server, after string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	url := server.URL + "/api/projects/" + projectID + "/runs/" + runID + "/events"
	if after != "" {
		url += "?afterSequence=" + after
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

type sseFrame struct {
	Name  string
	ID    string
	Event EventEvidenceDTO
	Raw   string
}

func readSSEFrames(t *testing.T, body io.Reader, n int) []sseFrame {
	t.Helper()
	reader := bufio.NewReader(body)
	frames := make([]sseFrame, 0, n)
	deadline := time.Now().Add(2 * time.Second)
	var buf bytes.Buffer
	for len(frames) < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d SSE frames, got %d (%s)", n, len(frames), buf.String())
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE: %v buf=%s", err, buf.String())
		}
		buf.WriteString(line)
		if line != "\n" && line != "\r\n" {
			continue
		}
		frame, ok := parseSSEFrame(buf.String())
		buf.Reset()
		if !ok {
			continue
		}
		frames = append(frames, frame)
	}
	return frames
}

func parseSSEFrame(block string) (sseFrame, bool) {
	var frame sseFrame
	var data strings.Builder
	for _, line := range strings.Split(strings.TrimRight(block, "\r\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "id:"):
			frame.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			frame.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case strings.HasPrefix(strings.TrimSpace(line), ":"):
			return sseFrame{}, false
		}
	}
	if data.Len() == 0 {
		return sseFrame{}, false
	}
	if frame.Name == "resync" {
		frame.Raw = block
		return frame, true
	}
	if err := json.Unmarshal([]byte(data.String()), &frame.Event); err != nil {
		return sseFrame{}, false
	}
	if frame.ID == "" {
		frame.ID = frame.Event.ID
	}
	frame.Raw = block
	return frame, true
}

type publishingQuestionStore struct {
	apiQuestionStore
	events []store.Event
}

func (s *publishingQuestionStore) AnswerQuestion(ctx context.Context, command store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	result, err := s.apiQuestionStore.AnswerQuestion(ctx, command)
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}
	result.Events = append([]store.Event(nil), s.events...)
	return result, nil
}

type discardingEventStore struct{}

func (discardingEventStore) AppendEvent(_ context.Context, event store.Event) (store.Event, error) {
	return event, nil
}
