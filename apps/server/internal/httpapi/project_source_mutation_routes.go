package httpapi

import "github.com/go-chi/chi/v5"

func (a *api) registerProjectSourceMutationRoutes(r chi.Router) {
	r.Post("/projects/{projectID}/source-connections", a.createProjectSourceConnection)
}
