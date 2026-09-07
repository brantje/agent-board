package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestClientNativeSessionAndQuestionRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model ModelRef `json:"model"`
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode create session: %v", err)
		}
		if payload.Model.ProviderID != "anthropic" || payload.Model.ID != "claude-sonnet" || payload.Location.Directory != "/workspace" {
			t.Errorf("create payload=%+v", payload)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"data": map[string]any{"id": "ses_1"}})
	})
	mux.HandleFunc("POST /api/session/ses_1/prompt", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Prompt struct {
				Text string `json:"text"`
			} `json:"prompt"`
			Delivery string `json:"delivery"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode prompt: %v", err)
		}
		if payload.Prompt.Text != "implement issue" || payload.Delivery != "steer" {
			t.Errorf("prompt payload=%+v", payload)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"data": map[string]any{"id": "input_1"}})
	})
	mux.HandleFunc("POST /api/session/ses_1/interrupt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /api/session/ses_1/question", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"data": []any{map[string]any{
			"id": "que_1", "sessionID": "ses_1", "questions": []any{
				map[string]any{"question": "Pick one", "header": "Choice", "options": []any{map[string]any{"label": "A", "description": "first"}}},
				map[string]any{"question": "More?", "header": "Many", "options": []any{map[string]any{"label": "B", "description": "second"}}, "multiple": true},
			},
		}}})
	})
	mux.HandleFunc("POST /api/session/ses_1/question/que_1/reply", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Answers [][]string `json:"answers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode reply: %v", err)
		}
		want := [][]string{{"A"}, {"B", "custom"}}
		if !reflect.DeepEqual(payload.Answers, want) {
			t.Errorf("answers=%v want=%v", payload.Answers, want)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/session/ses_1/question/que_1/reject", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}

	session, err := client.CreateSession(context.Background(), CreateSessionRequest{Directory: "/workspace", Model: ModelRef{ProviderID: "anthropic", ID: "claude-sonnet"}})
	if err != nil || session.ID != "ses_1" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if err := client.Prompt(context.Background(), session.ID, "implement issue"); err != nil {
		t.Fatal(err)
	}
	requests, err := client.ListQuestions(context.Background(), session.ID)
	if err != nil || len(requests) != 1 || len(requests[0].Questions) != 2 || requests[0].ID != "que_1" {
		t.Fatalf("questions=%+v err=%v", requests, err)
	}
	if requests[0].Questions[1].Multiple == nil || !*requests[0].Questions[1].Multiple {
		t.Fatalf("multiple=%v", requests[0].Questions[1].Multiple)
	}
	if err := client.ReplyQuestion(context.Background(), session.ID, "que_1", [][]string{{"A"}, {"B", "custom"}}); err != nil {
		t.Fatal(err)
	}
	if err := client.RejectQuestion(context.Background(), session.ID, "que_1"); err != nil {
		t.Fatal(err)
	}
	if err := client.InterruptSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestClientHealthFallsBackToLegacyRoute(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	mux.HandleFunc("GET /global/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, Health{Healthy: true, Version: "1.2.3"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	health, err := client.Health(context.Background())
	if err != nil || !health.Healthy || health.Version != "1.2.3" {
		t.Fatalf("health=%+v err=%v", health, err)
	}
}

func TestClientDecodesNativeSSEEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/event" || r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("request path=%q accept=%q", r.URL.Path, r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": heartbeat\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"evt_1\",\"type\":\"question.v2.asked\",\"properties\":{\"id\":\"que_1\",\"sessionID\":\"ses_1\",\"questions\":[]}}\n\n")
	}))
	defer server.Close()
	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil || event.ID != "evt_1" || event.Type != "question.v2.asked" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	var request QuestionRequest
	if err := json.Unmarshal(event.Properties, &request); err != nil || request.ID != "que_1" || request.SessionID != "ses_1" {
		t.Fatalf("question=%+v err=%v", request, err)
	}
}

func TestClientReturnsTypedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()
	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListQuestions(context.Background(), "ses_1")
	var httpErr *HTTPError
	if !errorsAs(err, &httpErr) || httpErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("err=%v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// errorsAs keeps this test file's imports small while still exercising the
// concrete HTTPError contract.
func errorsAs(err error, target any) bool {
	for err != nil {
		if candidate, ok := err.(*HTTPError); ok {
			pointer, ok := target.(**HTTPError)
			if ok {
				*pointer = candidate
				return true
			}
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
