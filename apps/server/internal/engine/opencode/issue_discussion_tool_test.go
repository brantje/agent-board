package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type recordingIssueDiscussionReader struct {
	mu       sync.Mutex
	requests []engine.IssueDiscussionReadRequest
	result   engine.IssueDiscussionReadResult
	err      error
}

func (r *recordingIssueDiscussionReader) ReadIssueDiscussion(_ context.Context, request engine.IssueDiscussionReadRequest) (engine.IssueDiscussionReadResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request)
	return r.result, r.err
}

func (r *recordingIssueDiscussionReader) Requests() []engine.IssueDiscussionReadRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]engine.IssueDiscussionReadRequest(nil), r.requests...)
}

func TestIssueDiscussionBridgeServesCanonicalReadSynchronously(t *testing.T) {
	body := "server-owned finding"
	reader := &recordingIssueDiscussionReader{result: engine.IssueDiscussionReadResult{
		Mode: engine.IssueDiscussionReadRecent,
		Roots: []engine.IssueDiscussionRoot{{
			Root: engine.IssueDiscussionComment{
				ID: "comment-1", Body: &body,
				Author: engine.IssueDiscussionAuthor{Type: "AGENT", ID: "agent-1", Name: "Agent"},
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			},
		}},
	}}

	request := issueDiscussionBridgeRequest{
		ID: "call-1",
		Request: engine.IssueDiscussionReadRequest{
			Mode: engine.IssueDiscussionReadRecent, Limit: 5,
		},
	}
	responseCh := make(chan issueDiscussionBridgeResponse, 1)
	var mu sync.Mutex
	responded := false
	nextCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /next", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		nextCalls++
		if responded {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(request); err != nil {
			t.Fatal(err)
		}
	})
	mux.HandleFunc("POST /respond", func(w http.ResponseWriter, r *http.Request) {
		var response issueDiscussionBridgeResponse
		if err := json.NewDecoder(r.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		responded = true
		mu.Unlock()
		responseCh <- response
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	bridge := &issueDiscussionBridge{http: server.Client(), baseURL: server.URL, reader: reader}
	if err := waitIssueDiscussionBridgeHealthy(t.Context(), bridge); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- bridge.Serve(ctx) }()

	var response issueDiscussionBridgeResponse
	select {
	case response = <-responseCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for synchronous discussion response")
	}
	if response.ID != "call-1" || response.Error != "" || response.Result == nil || response.Result.Mode != engine.IssueDiscussionReadRecent {
		t.Fatalf("response=%+v", response)
	}
	if len(response.Result.Roots) != 1 || response.Result.Roots[0].Root.Body == nil || *response.Result.Roots[0].Root.Body != body {
		t.Fatalf("result=%+v", response.Result)
	}
	requests := reader.Requests()
	if len(requests) != 1 || requests[0].Mode != engine.IssueDiscussionReadRecent || requests[0].Limit != 5 {
		t.Fatalf("requests=%+v", requests)
	}
	mu.Lock()
	callsBeforeCancel := nextCalls
	mu.Unlock()
	if callsBeforeCancel < 1 {
		t.Fatalf("next calls=%d", callsBeforeCancel)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge shutdown error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop after cancellation")
	}
}

func TestIssueDiscussionToolSourceUsesReplaySafeSynchronousBridge(t *testing.T) {
	for _, want := range []string{
		`createServer`,
		`return await bridge.enqueue`,
		`response.end(JSON.stringify(queued[0]))`,
		`queued.splice(queuedIndex, 1)`,
		`mode: tool.schema.enum(["recent", "thread", "updates"])`,
	} {
		if !strings.Contains(issueDiscussionToolSource, want) {
			t.Fatalf("tool source missing %q", want)
		}
	}
	for _, forbidden := range []string{"follow-up result", "stop the current turn"} {
		if strings.Contains(strings.ToLower(issueDiscussionToolSource), forbidden) {
			t.Fatalf("tool source still contains asynchronous delivery instruction %q", forbidden)
		}
	}
}

func TestIssueDiscussionCapabilityIsInstalledAndMatched(t *testing.T) {
	command := openCodeServeCommandWithDiscussion("127.0.0.1", "4100", false, false, true, false)
	if len(command) != 10 || command[0] != "sh" || command[1] != "-c" {
		t.Fatalf("command=%q", command)
	}
	if command[8] != issueDiscussionToolSource || !strings.Contains(command[2], "read_issue_discussion.ts") {
		t.Fatalf("discussion tool not installed: %q", command)
	}
	if !openCodeToolCapabilitiesMatchWithDiscussion([]string{issueDiscussionToolName}, false, false, true, false) {
		t.Fatal("matching discussion capability was rejected")
	}
	if openCodeToolCapabilitiesMatchWithDiscussion([]string{issueDiscussionToolName}, false, false, false, false) {
		t.Fatal("unexpected discussion capability was accepted")
	}
	if openCodeToolCapabilitiesMatchWithDiscussion(nil, false, false, true, false) {
		t.Fatal("missing discussion capability was accepted")
	}
}

func TestIssueDiscussionBridgeAddressIsStableAndSeparate(t *testing.T) {
	native := "127.0.0.1:32000"
	first, err := issueDiscussionBridgeAddress(native, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := issueDiscussionBridgeAddress(native, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	other, err := issueDiscussionBridgeAddress(native, "run-2")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first == native || first == other {
		t.Fatalf("first=%q second=%q native=%q other=%q", first, second, native, other)
	}
	if _, err := issueDiscussionBridgeAddress("not-an-address", "run-1"); err == nil {
		t.Fatal("invalid native address unexpectedly accepted")
	}
}

func TestBoundedIssueDiscussionToolError(t *testing.T) {
	long := strings.Repeat("x", 2048)
	if got := boundedIssueDiscussionToolError(context.Canceled); got != context.Canceled.Error() {
		t.Fatalf("error=%q", got)
	}
	if got := boundedIssueDiscussionToolError(&testDiscussionError{message: long}); len(got) != 1024 {
		t.Fatalf("bounded error length=%d", len(got))
	}
}

type testDiscussionError struct{ message string }

func (e *testDiscussionError) Error() string { return e.message }
