package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientConstructionAndMethodValidation(t *testing.T) {
	if _, err := New(nil, "http://opencode.local"); err == nil {
		t.Fatal("nil HTTP client unexpectedly accepted")
	}
	for _, baseURL := range []string{"", "localhost:4096", "://bad"} {
		if _, err := New(http.DefaultClient, baseURL); err == nil {
			t.Fatalf("invalid base URL %q unexpectedly accepted", baseURL)
		}
	}

	native, err := New(http.DefaultClient, "http://opencode.local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := native.CreateSession(context.Background(), CreateSessionRequest{}); err == nil {
		t.Fatal("empty CreateSession request unexpectedly accepted")
	}
	if err := native.Prompt(context.Background(), "", "prompt"); err == nil {
		t.Fatal("empty Prompt session unexpectedly accepted")
	}
	if err := native.Prompt(context.Background(), "ses_1", " "); err == nil {
		t.Fatal("empty Prompt text unexpectedly accepted")
	}
	if err := native.WaitSession(context.Background(), " "); err == nil {
		t.Fatal("empty WaitSession id unexpectedly accepted")
	}
	if err := native.InterruptSession(context.Background(), " "); err == nil {
		t.Fatal("empty InterruptSession id unexpectedly accepted")
	}
	if _, err := native.ListQuestions(context.Background(), " "); err == nil {
		t.Fatal("empty ListQuestions id unexpectedly accepted")
	}
	if err := native.ReplyQuestion(context.Background(), "", "que_1", nil); err == nil {
		t.Fatal("empty ReplyQuestion session unexpectedly accepted")
	}
	if err := native.ReplyQuestion(context.Background(), "ses_1", "", nil); err == nil {
		t.Fatal("empty ReplyQuestion request unexpectedly accepted")
	}
	if err := native.RejectQuestion(context.Background(), "", "que_1"); err == nil {
		t.Fatal("empty RejectQuestion session unexpectedly accepted")
	}
	if err := native.RejectQuestion(context.Background(), "ses_1", ""); err == nil {
		t.Fatal("empty RejectQuestion request unexpectedly accepted")
	}
}

func TestClientUnavailableAndEncodingFailures(t *testing.T) {
	var native *Client
	if _, err := native.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("nil Health() error=%v", err)
	}

	native, err := New(http.DefaultClient, "http://opencode.local")
	if err != nil {
		t.Fatal(err)
	}
	if err := native.doJSON(context.Background(), http.MethodPost, "/api/test", make(chan int), nil); err == nil || !strings.Contains(err.Error(), "encode") {
		t.Fatalf("unsupported payload error=%v", err)
	}
}

func TestClientRejectsUnhealthyAndInvalidJSONResponses(t *testing.T) {
	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			writeJSON(t, w, http.StatusOK, Health{Healthy: false})
		default:
			http.NotFound(w, r)
		}
	}))
	defer unhealthy.Close()
	native, err := New(unhealthy.Client(), unhealthy.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := native.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("unhealthy Health() error=%v", err)
	}

	invalidJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	defer invalidJSON.Close()
	native, err = New(invalidJSON.Client(), invalidJSON.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := native.ListQuestions(context.Background(), "ses_1"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("invalid JSON error=%v", err)
	}
}
