package httpapi

import "github.com/go-chi/chi/v5"

// Source routes are registered with configuration routes.
func (a *api) registerSourceRoutes(r chi.Router) {
	registerProject := a.registerProjectSourceRoutes
	registerProject(r)
	r.Get("/source-connections", a.listGlobalSourceConnections)
	r.Post("/source-connections", a.createGlobalSourceConnection)
	r.Get("/source-connections/{resourceID}", a.getGlobalSourceConnection)
	r.Put("/source-connections/{resourceID}", a.updateGlobalSourceConnection)
	r.Get("/source-connections/{resourceID}/repositories", a.listGlobalSourceRepositories)
}
