package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplicationHandlerRoutesMCPSeparately(t *testing.T) {
	baseCalls := 0
	handler := &applicationHandler{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		baseCalls++
		w.WriteHeader(http.StatusNoContent)
	})}

	regular := httptest.NewRecorder()
	handler.ServeHTTP(regular, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if regular.Code != http.StatusNoContent || baseCalls != 1 {
		t.Fatalf("regular route status=%d baseCalls=%d", regular.Code, baseCalls)
	}

	mcpResponse := httptest.NewRecorder()
	handler.ServeHTTP(mcpResponse, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if mcpResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("MCP route status=%d body=%s", mcpResponse.Code, mcpResponse.Body.String())
	}
	if baseCalls != 1 {
		t.Fatalf("MCP request reached base HTTP router; baseCalls=%d", baseCalls)
	}
}
