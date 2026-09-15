package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMountMCPHandlerRoutesSeparately(t *testing.T) {
	baseCalls := 0
	mcpCalls := 0
	handler := mountMCPHandler(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			baseCalls++
			w.WriteHeader(http.StatusNoContent)
		}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mcpCalls++
			w.WriteHeader(http.StatusAccepted)
		}),
	)

	regular := httptest.NewRecorder()
	handler.ServeHTTP(regular, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if regular.Code != http.StatusNoContent || baseCalls != 1 || mcpCalls != 0 {
		t.Fatalf("regular route status=%d baseCalls=%d mcpCalls=%d", regular.Code, baseCalls, mcpCalls)
	}

	for range 2 {
		mcpResponse := httptest.NewRecorder()
		handler.ServeHTTP(mcpResponse, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		if mcpResponse.Code != http.StatusAccepted {
			t.Fatalf("MCP route status=%d body=%s", mcpResponse.Code, mcpResponse.Body.String())
		}
	}
	if baseCalls != 1 || mcpCalls != 2 {
		t.Fatalf("MCP routing baseCalls=%d mcpCalls=%d", baseCalls, mcpCalls)
	}
}

func TestNewApplicationHandlerFailsWhenMCPServicesAreIncomplete(t *testing.T) {
	if _, err := newApplicationHandler(http.NotFoundHandler(), nil); err == nil {
		t.Fatal("newApplicationHandler returned nil error for incomplete MCP services")
	}
}
