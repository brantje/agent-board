package httpapi

import "github.com/go-chi/chi/v5"

// Source routes are registered with configuration routes.
func (a *api) registerSourceRoutes(r chi.Router) {
	r.Get("/source-connections", a.listGlobalSourceConnections)
	r.Post("/source-connections", a.createGlobalSourceConnection)
	r.Get("/source-connections/{resourceID}", a.getGlobalSourceConnection)
	r.Put("/source-connections/{resourceID}", a.updateGlobalSourceConnection)
}
