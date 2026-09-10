package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientPromptUsesDocumentedHeadlessAsyncRoute(t *testing.T) {
	var stableCalls atomic.Int32
	var fallbackCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /session/ses_1/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		stableCalls.Add(1)
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
	mux.HandleFunc("POST /api/session/ses_1/prompt", func(w http.ResponseWriter, _ *http.Request) {
		fallbackCalls.Add(1)
		http.Error(w, "unexpected V2 fallback", http.StatusInternalServerError)
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
	if calls := stableCalls.Load(); calls != 1 {
		t.Fatalf("prompt_async calls=%d want 1", calls)
	}
	if calls := fallbackCalls.Load(); calls != 0 {
		t.Fatalf("V2 fallback calls=%d want 0", calls)
	}
}
