package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestReplyNativeQuestionTreatsMissingPendingRequestAsAccepted(t *testing.T) {
	replyCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/session/ses_1/question/que_1/reply":
			replyCalls++
			http.Error(w, "connection outcome unknown", http.StatusBadGateway)
		case "GET /api/session/ses_1/question":
			writeNativeJSON(t, w, map[string]any{"data": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	native := mustNativeQuestionClient(t, server)

	if err := replyNativeQuestion(context.Background(), native, "ses_1", "que_1", [][]string{{"A"}}); err != nil {
		t.Fatalf("replyNativeQuestion() error=%v", err)
	}
	if replyCalls != 1 {
		t.Fatalf("reply calls=%d want 1", replyCalls)
	}
}

func TestReplyNativeQuestionRetriesOnlyWhileRequestIsPending(t *testing.T) {
	replyCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/session/ses_1/question/que_1/reply":
			replyCalls++
			if replyCalls == 1 {
				http.Error(w, "temporary failure", http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "GET /api/session/ses_1/question":
			writeNativeJSON(t, w, map[string]any{"data": []any{map[string]any{
				"id": "que_1", "sessionID": "ses_1", "questions": []any{map[string]any{"question": "Choose", "header": "Choice", "options": []any{map[string]any{"label": "A", "description": "first"}}},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	native := mustNativeQuestionClient(t, server)

	if err := replyNativeQuestion(context.Background(), native, "ses_1", "que_1", [][]string{{"A"}}); err != nil {
		t.Fatalf("replyNativeQuestion() error=%v", err)
	}
	if replyCalls != 2 {
		t.Fatalf("reply calls=%d want 2", replyCalls)
	}
}

func TestReplyNativeQuestionSurfacesReconciliationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/session/ses_1/question/que_1/reply":
			http.Error(w, "temporary failure", http.StatusBadGateway)
		case "GET /api/session/ses_1/question":
			http.Error(w, "list unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	native := mustNativeQuestionClient(t, server)

	if err := replyNativeQuestion(context.Background(), native, "ses_1", "que_1", [][]string{{"A"}}); err == nil {
		t.Fatal("reconciliation failure unexpectedly ignored")
	}
}

func TestActiveNativeRequestsContainsOnlyUnresolvedRequests(t *testing.T) {
	state := newRunState("ses_1", nil, nil)
	state.nativeQuestions["open"] = &nativeQuestionState{}
	state.nativeQuestions["answered"] = &nativeQuestionState{answered: true}
	state.nativeQuestions["replied"] = &nativeQuestionState{answered: true, replied: true}
	state.nativeQuestions["nil"] = nil

	active := state.activeNativeRequests()
	if _, ok := active["open"]; !ok {
		t.Fatal("open request missing")
	}
	if _, ok := active["answered"]; !ok {
		t.Fatal("answered but not replied request missing")
	}
	if _, ok := active["replied"]; ok {
		t.Fatal("replied request still active")
	}
	if _, ok := active["nil"]; ok {
		t.Fatal("nil request state still active")
	}
}

func mustNativeQuestionClient(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return native
}
