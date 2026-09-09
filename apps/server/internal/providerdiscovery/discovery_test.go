package providerdiscovery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestResolveBaseURLUsesProviderBaseURL(t *testing.T) {
	base := "https://example.com/v1/"
	got, err := ResolveBaseURL(store.Provider{Kind: "custom", BaseURL: &base})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got != "https://example.com/v1" {
		t.Fatalf("baseURL=%q", got)
	}
}

func TestResolveBaseURLUsesBuiltInDefault(t *testing.T) {
	got, err := ResolveBaseURL(store.Provider{Kind: "openai"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got != "https://api.openai.com/v1" {
		t.Fatalf("baseURL=%q", got)
	}
}

func TestResolveBaseURLUsesOpenRouterDefault(t *testing.T) {
	got, err := ResolveBaseURL(store.Provider{Kind: "openrouter"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got != "https://openrouter.ai/api/v1" {
		t.Fatalf("baseURL=%q", got)
	}
}

func TestResolveBaseURLRequiresBaseURLForCustomProvider(t *testing.T) {
	_, err := ResolveBaseURL(store.Provider{Kind: "llamarack"})
	if err == nil || !strings.Contains(err.Error(), "baseUrl") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveBaseURLRequiresBaseURLForBuiltInWithoutDefault(t *testing.T) {
	_, err := ResolveBaseURL(store.Provider{Kind: "anthropic"})
	if err == nil || !strings.Contains(err.Error(), "default models endpoint") {
		t.Fatalf("err=%v", err)
	}
}

func TestListModelsParsesOpenAIResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret-key" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "gpt-z"},
				{"id": "gpt-a"},
				{"id": "gpt-a"},
			},
		})
	}))
	defer server.Close()

	client := &http.Client{}
	models, err := ListModels(context.Background(), client, server.URL, []byte("secret-key"))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(models) != 2 || models[0].ID != "gpt-a" || models[1].ID != "gpt-z" {
		t.Fatalf("models=%v", models)
	}
}

func TestListModelsRejectsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "not-json")
	}))
	defer server.Close()

	_, err := ListModels(context.Background(), server.Client(), server.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("err=%v", err)
	}
}

func TestListModelsRequiresHTTPClient(t *testing.T) {
	_, err := ListModels(context.Background(), nil, "https://example.com/v1", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListModelsIncludesDisplayName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "gpt-a", "name": "GPT A"}},
		})
	}))
	defer server.Close()

	models, err := ListModels(context.Background(), server.Client(), server.URL, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if models[0].Name == nil || *models[0].Name != "GPT A" {
		t.Fatalf("models=%v", models)
	}
}

func TestListModelsRejectsUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer server.Close()

	_, err := ListModels(context.Background(), server.Client(), server.URL, []byte("secret-key"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDiscoverUsesProviderBaseURLAndCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-a"}},
		})
	}))
	defer server.Close()

	base := server.URL
	models, err := Discover(context.Background(), server.Client(), store.Provider{Kind: "custom", BaseURL: &base}, []byte("secret"))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(models) != 1 || models[0].ID != "model-a" {
		t.Fatalf("models=%v", models)
	}
}

func TestDiscoverRejectsUnconfiguredProvider(t *testing.T) {
	_, err := Discover(context.Background(), nil, store.Provider{Kind: "custom"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestModelsEndpointJoinsBaseURL(t *testing.T) {
	if got := modelsEndpoint("https://api.example.com/v1"); got != "https://api.example.com/v1/models" {
		t.Fatalf("endpoint=%q", got)
	}
	if got := modelsEndpoint("https://api.example.com/v1/"); got != "https://api.example.com/v1/models" {
		t.Fatalf("endpoint=%q", got)
	}
}
