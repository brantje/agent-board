package httpapi

import "github.com/go-chi/chi/v5"

func (a *api) registerProjectSourceRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/source-connections", a.listProjectSourceConnections)
}
