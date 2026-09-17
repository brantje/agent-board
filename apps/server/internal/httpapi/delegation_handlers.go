package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (a *api) registerDelegationRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/runs/{runID}/delegations", a.listDelegationsByParentRun)
	r.Get("/projects/{projectID}/runs/{runID}/delegation", a.getDelegationByRun)
}

func (a *api) listDelegationsByParentRun(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	parentRunID, ok := pathUUID(w, r, "runID")
	if !ok {
		return
	}
	values, err := a.service.ListDelegationsByParentRun(r.Context(), projectID, parentRunID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]DelegationDTO, 0, len(values))
	for _, value := range values {
		out = append(out, delegationDTO(value, keys))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) getDelegationByRun(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	runID, ok := pathUUID(w, r, "runID")
	if !ok {
		return
	}
	value, err := a.service.GetDelegationByRun(r.Context(), projectID, runID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, delegationDTO(value, keys))
}
