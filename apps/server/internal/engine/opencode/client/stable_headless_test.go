package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestClientUsesV2LifecycleAndSharedQuestionRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/session/active", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"ses_1": map[string]any{"type": "running"},
			},
		})
	})
	mux.HandleFunc("POST /api/session/ses_1/interrupt", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /question", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, []any{
			map[string]any{"id": "que_other", "sessionID": "ses_other", "questions": []any{}},
			map[string]any{
				"id": "que_1", "sessionID": "ses_1", "questions": []any{
					map[string]any{"question": "Pick one", "header": "Choice", "options": []any{map[string]any{"label": "A", "description": "first"}}},
				},
			},
		})
	})
	mux.HandleFunc("POST /question/que_1/reply", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Answers [][]string `json:"answers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode reply: %v", err)
		}
		if want := [][]string{{"A"}}; !reflect.DeepEqual(payload.Answers, want) {
			t.Errorf("answers=%v want=%v", payload.Answers, want)
		}
		writeJSON(t, w, http.StatusOK, true)
	})
	mux.HandleFunc("POST /question/que_1/reject", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, true)
	})

	mux.HandleFunc("GET /session/status", unexpectedLegacyFallback(t))
	mux.HandleFunc("POST /session/ses_1/abort", unexpectedLegacyFallback(t))
	mux.HandleFunc("GET /api/session/ses_1/question", unexpectedLegacyFallback(t))
	mux.HandleFunc("POST /api/session/ses_1/question/que_1/reply", unexpectedLegacyFallback(t))
	mux.HandleFunc("POST /api/session/ses_1/question/que_1/reject", unexpectedLegacyFallback(t))

	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}

	active, err := native.SessionActive(context.Background(), "ses_1")
	if err != nil || !active {
		t.Fatalf("ses_1 active=%v err=%v", active, err)
	}
	active, err = native.SessionActive(context.Background(), "ses_other")
	if err != nil || active {
		t.Fatalf("ses_other active=%v err=%v", active, err)
	}
	requests, err := native.ListQuestions(context.Background(), "ses_1")
	if err != nil || len(requests) != 1 || requests[0].ID != "que_1" {
		t.Fatalf("questions=%+v err=%v", requests, err)
	}
	if err := native.ReplyQuestion(context.Background(), "ses_1", "que_1", [][]string{{"A"}}); err != nil {
		t.Fatal(err)
	}
	if err := native.RejectQuestion(context.Background(), "ses_1", "que_1"); err != nil {
		t.Fatal(err)
	}
	if err := native.InterruptSession(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
}

func unexpectedLegacyFallback(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected legacy fallback %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected legacy fallback", http.StatusInternalServerError)
	}
}

func TestSessionActiveFallsBackToLegacyWhenV2RouteMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/session/active", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"ses_1": map[string]any{"type": "busy"},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	active, err := native.SessionActive(context.Background(), "ses_1")
	if err != nil || !active {
		t.Fatalf("ses_1 active=%v err=%v", active, err)
	}
	active, err = native.SessionActive(context.Background(), "ses_missing")
	if err != nil || active {
		t.Fatalf("missing session active=%v err=%v", active, err)
	}
}

func TestSessionActiveTreatsV2AbsenceAsIdleWithoutLegacyFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/session/active", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"data": map[string]any{}})
	})
	mux.HandleFunc("GET /session/status", unexpectedLegacyFallback(t))
	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	active, err := native.SessionActive(context.Background(), "ses_1")
	if err != nil || active {
		t.Fatalf("ses_1 active=%v err=%v", active, err)
	}
}

func TestInterruptFallsBackToLegacyOnlyWhenV2RouteMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session/ses_1/interrupt", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("POST /session/ses_1/abort", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, true)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.InterruptSession(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
}
