package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientScopesNativeSessionAPIsToBoundDirectory(t *testing.T) {
	const boundDir = "/tmp/external-runner-workspaces/session-1"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
		assertBoundDirectory(t, r, boundDir)
		writeJSON(t, w, http.StatusOK, []any{map[string]any{
			"id":        "ses_host",
			"directory": boundDir,
		}})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, r *http.Request) {
		assertBoundDirectory(t, r, boundDir)
		writeJSON(t, w, http.StatusOK, map[string]any{
			"ses_host": map[string]any{"type": "busy"},
		})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		assertBoundDirectory(t, r, boundDir)
		var payload struct {
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode create session: %v", err)
		}
		if payload.Location.Directory != boundDir {
			t.Errorf("create location.directory=%q want %q", payload.Location.Directory, boundDir)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"data": map[string]any{"id": "ses_new", "directory": boundDir}})
	})
	mux.HandleFunc("POST /session/ses_host/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		assertBoundDirectory(t, r, boundDir)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /event", func(w http.ResponseWriter, r *http.Request) {
		assertBoundDirectory(t, r, boundDir)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"connected\",\"type\":\"server.connected\"}\n\n"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	native.SetDirectory(boundDir)

	sessions, err := native.ListSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].ID != "ses_host" {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	active, err := native.SessionActive(context.Background(), "ses_host")
	if err != nil || !active {
		t.Fatalf("active=%v err=%v", active, err)
	}
	created, err := native.CreateSession(context.Background(), CreateSessionRequest{Model: ModelRef{ProviderID: "openrouter", ID: "free"}})
	if err != nil || created.ID != "ses_new" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	if err := native.Prompt(context.Background(), "ses_host", "edit answer.go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	stream, err := native.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	_ = stream.Close()
}

func assertBoundDirectory(t *testing.T, r *http.Request, want string) {
	t.Helper()
	if got := r.URL.Query().Get("directory"); got != want {
		t.Errorf("%s %s directory query=%q want %q", r.Method, r.URL.Path, got, want)
	}
	if got := r.Header.Get("x-opencode-directory"); got != want {
		t.Errorf("%s %s x-opencode-directory=%q want %q", r.Method, r.URL.Path, got, want)
	}
}
