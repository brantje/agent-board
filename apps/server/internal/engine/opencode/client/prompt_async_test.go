package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientPromptUsesV2SessionLifecycle(t *testing.T) {
	var v2Calls atomic.Int32
	var legacyCalls atomic.Int32
	mux := http.NewServeMux()
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
	mux.HandleFunc("POST /session/ses_1/prompt_async", func(w http.ResponseWriter, _ *http.Request) {
		legacyCalls.Add(1)
		http.Error(w, "unexpected legacy fallback", http.StatusInternalServerError)
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
	if calls := legacyCalls.Load(); calls != 0 {
		t.Fatalf("legacy fallback calls=%d want 0", calls)
	}
}

func TestClientPromptFallsBackToLegacyOnlyWhenV2RouteMissing(t *testing.T) {
	var legacyCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session/ses_1/prompt", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("POST /session/ses_1/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		legacyCalls.Add(1)
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

	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.Prompt(context.Background(), "ses_1", "implement issue"); err != nil {
		t.Fatal(err)
	}
	if calls := legacyCalls.Load(); calls != 1 {
		t.Fatalf("legacy prompt_async calls=%d want 1", calls)
	}
}

func TestClientPromptDoesNotMaskV2FailureWithLegacyFallback(t *testing.T) {
	var legacyCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session/ses_1/prompt", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "v2 failed", http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /session/ses_1/prompt_async", func(w http.ResponseWriter, _ *http.Request) {
		legacyCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.Prompt(context.Background(), "ses_1", "implement issue"); err == nil {
		t.Fatal("V2 prompt failure was masked")
	}
	if calls := legacyCalls.Load(); calls != 0 {
		t.Fatalf("legacy fallback calls=%d want 0", calls)
	}
}
