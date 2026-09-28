package httpapi

import "github.com/go-chi/chi/v5"

func (a *api) registerProjectSourceRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/source-connections", a.listProjectSourceConnections)
	r.Post("/projects/{projectID}/source-connections", a.createProjectSourceConnection)
	r.Get("/projects/{projectID}/source-connections/{resourceID}", a.getProjectSourceConnection)
	r.Put("/projects/{projectID}/source-connections/{resourceID}", a.updateProjectSourceConnection)
	r.Get("/projects/{projectID}/source-connections/{resourceID}/repositories", a.listProjectSourceRepositories)
	r.Get("/projects/{projectID}/source-connections/{resourceID}/repositories/{repositoryID}", a.getProjectSourceRepository)
}
