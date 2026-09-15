package main

import (
	"log/slog"
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/mcpapi"
)

func (a *applicationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/mcp" {
		a.Handler.ServeHTTP(w, r)
		return
	}

	handler, err := mcpapi.NewHandler(a.services)
	if err != nil {
		slog.Error("initialize MCP transport", "error", err)
		http.Error(w, "MCP unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, r)
}
