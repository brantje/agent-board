package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientUsesLegacySessionAndPromptLifecycle(t *testing.T) {
	var legacyCreateCalls atomic.Int32
	var legacyPromptCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		legacyCreateCalls.Add(1)
		if got := r.URL.Query().Get("directory"); got != "/workspace" {
			t.Errorf("directory=%q want /workspace", got)
		}
		var payload struct {
			Model ModelRef `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode legacy create payload: %v", err)
			return
		}
		if payload.Model.ProviderID != "openrouter" || payload.Model.ID != "test-model" {
			t.Errorf("legacy create model=%+v", payload.Model)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"id": "ses_1"})
	})
	mux.HandleFunc("POST /session/ses_1/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		legacyPromptCalls.Add(1)
		var payload struct {
			Parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"parts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode prompt_async payload: %v", err)
			return
		}
		if len(payload.Parts) != 1 || payload.Parts[0].Type != "text" || payload.Parts[0].Text != "implement issue" {
			t.Errorf("prompt_async payload=%+v", payload)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/session", unexpectedV2Fallback(t))
	mux.HandleFunc("POST /api/session/ses_1/prompt", unexpectedV2Fallback(t))

	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	session, err := native.CreateSession(context.Background(), CreateSessionRequest{
		Directory: "/workspace",
		Model:     ModelRef{ProviderID: "openrouter", ID: "test-model"},
	})
	if err != nil || session.ID != "ses_1" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if err := native.Prompt(context.Background(), session.ID, "implement issue"); err != nil {
		t.Fatal(err)
	}
	if calls := legacyCreateCalls.Load(); calls != 1 {
		t.Fatalf("legacy create calls=%d want 1", calls)
	}
	if calls := legacyPromptCalls.Load(); calls != 1 {
		t.Fatalf("legacy prompt calls=%d want 1", calls)
	}
}

func TestClientPromptFallsBackToV2OnlyWhenLegacyRouteMissing(t *testing.T) {
	var v2Calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /session/ses_1/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("POST /api/session/ses_1/prompt", func(w http.ResponseWriter, r *http.Request) {
		v2Calls.Add(1)
		var payload struct {
			Prompt struct {
				Text string `json:"text"`
			} `json:"prompt"`
			Delivery string `json:"delivery"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode V2 prompt payload: %v", err)
			return
		}
		if payload.Prompt.Text != "implement issue" || payload.Delivery != "steer" {
			t.Errorf("V2 prompt payload=%+v", payload)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.Prompt(context.Background(), "ses_1", "implement issue"); err != nil {
		t.Fatal(err)
	}
	if calls := v2Calls.Load(); calls != 1 {
		t.Fatalf("V2 prompt calls=%d want 1", calls)
	}
}

func TestClientPromptDoesNotMaskLegacyFailureWithV2Fallback(t *testing.T) {
	var v2Calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /session/ses_1/prompt_async", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "legacy failed", http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /api/session/ses_1/prompt", func(w http.ResponseWriter, _ *http.Request) {
		v2Calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.Prompt(context.Background(), "ses_1", "implement issue"); err == nil {
		t.Fatal("legacy prompt failure was masked")
	}
	if calls := v2Calls.Load(); calls != 0 {
		t.Fatalf("V2 fallback calls=%d want 0", calls)
	}
}

func unexpectedV2Fallback(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected V2 fallback %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected V2 fallback", http.StatusInternalServerError)
	}
}
