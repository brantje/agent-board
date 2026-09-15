package main

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/mcpapi"
)

func newApplicationHandler(base http.Handler, services *app.Services) (*applicationHandler, error) {
	mcpHandler, err := mcpapi.NewHandler(services)
	if err != nil {
		return nil, err
	}
	return &applicationHandler{
		Handler:  mountMCPHandler(base, mcpHandler),
		services: services,
	}, nil
}

func mountMCPHandler(base, mcpHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			mcpHandler.ServeHTTP(w, r)
			return
		}
		base.ServeHTTP(w, r)
	})
}
