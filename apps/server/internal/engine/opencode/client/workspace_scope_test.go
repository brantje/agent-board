package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientDoesNotScopeNativeSessionReadsToHardcodedWorkspace(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("directory"); got != "" {
			t.Errorf("session list directory=%q want omitted so host runner CWD sessions remain visible", got)
		}
		writeJSON(t, w, http.StatusOK, []any{map[string]any{
			"id":        "ses_host",
			"directory": "/tmp/external-runner-workspaces/session-1",
		}})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("directory"); got != "" {
			t.Errorf("session status directory=%q want omitted so host runner CWD sessions remain visible", got)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"ses_host": map[string]any{"type": "busy"},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := native.ListSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].ID != "ses_host" {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	active, err := native.SessionActive(context.Background(), "ses_host")
	if err != nil || !active {
		t.Fatalf("active=%v err=%v", active, err)
	}
}
