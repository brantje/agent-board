package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/go-chi/chi/v5"
)

func (a *api) registerDelegationRoutes(r chi.Router) {
	r.Post("/projects/{projectID}/runs/{runID}/delegations", a.createDelegation)
	r.Get("/projects/{projectID}/runs/{runID}/delegations", a.listDelegationsByParentRun)
	r.Get("/projects/{projectID}/runs/{runID}/delegation", a.getDelegationByRun)
}

func (a *api) createDelegation(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	parentRunID, ok := pathUUID(w, r, "runID")
	if !ok {
		return
	}
	var req CreateDelegationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := a.service.RequestDelegation(r.Context(), projectID, parentRunID, app.DelegationRequest{
		TargetAgentID: req.TargetAgentID,
		Task:          req.Task,
		RequestKey:    req.RequestKey,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, delegationDTO(result.Delegation, keys))
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
