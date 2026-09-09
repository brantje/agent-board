package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelContextLimitReadsRuntimeProviderMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"all":[{"id":"openrouter","models":{"model-a":{"limit":{"context":200000}}}}]}`))
	}))
	t.Cleanup(server.Close)
	client, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := client.ModelContextLimit(t.Context(), "openrouter", "model-a")
	if err != nil {
		t.Fatal(err)
	}
	if limit == nil || *limit != 200000 {
		t.Fatalf("limit=%v", limit)
	}
	unknown, err := client.ModelContextLimit(t.Context(), "openrouter", "unknown")
	if err != nil || unknown != nil {
		t.Fatalf("unknown limit=%v err=%v", unknown, err)
	}
}

func TestModelContextLimitValidatesSelection(t *testing.T) {
	client, err := New(http.DefaultClient, "http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ModelContextLimit(t.Context(), "", "model"); err == nil {
		t.Fatal("missing provider unexpectedly accepted")
	}
}
