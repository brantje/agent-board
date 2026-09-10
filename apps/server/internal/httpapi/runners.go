package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type RunnerDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Internal     bool            `json:"internal"`
	Managed      bool            `json:"managed"`
	Deletable    bool            `json:"deletable"`
	Connected    bool            `json:"connected"`
	RevokedAt    *time.Time      `json:"revokedAt"`
	LastSeenAt   *time.Time      `json:"lastSeenAt"`
	Capabilities json.RawMessage `json:"capabilities"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

func (a *api) runnerDTO(v store.Runner) RunnerDTO {
	caps := v.Capabilities
	if len(caps) == 0 {
		caps = store.EmptyObject
	}
	return RunnerDTO{ID: v.ID, Name: v.Name, Internal: v.Internal, Managed: v.Internal, Deletable: !v.Internal, Connected: a.service.Runners.Connections.Connected(v.ID), RevokedAt: v.RevokedAt, LastSeenAt: v.LastSeenAt, Capabilities: caps, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

type runnerNameRequest struct {
	Name string `json:"name"`
}
type runnerRegistrationRequest struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}
type runnerRegistrationResponse struct {
	Token string `json:"token"`
}
type runnerCredentialResponse struct {
	Runner RunnerDTO `json:"runner"`
	Token  string    `json:"token"`
}

func (a *api) registerRunnerRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/runners", a.getProjectRunners)
	r.Put("/projects/{projectID}/runners", a.setProjectRunners)
	r.Post("/runner/register", a.registerRunner)
	r.Handle("/runner/ws", a.service.Runners.Connections)
	r.Get("/runners", a.listRunners)
	r.Post("/runners", a.createRunnerRegistration)
	r.Get("/runners/{resourceID}", a.getRunner)
	r.Patch("/runners/{resourceID}", a.renameRunner)
	r.Post("/runners/{resourceID}/rotate-token", a.rotateRunner)
	r.Post("/runners/{resourceID}/revoke", a.revokeRunner)
	r.Delete("/runners/{resourceID}", a.deleteRunner)
}

type projectRunnersRequest struct {
	RunnerIDs []string `json:"runnerIds"`
}

func (a *api) getProjectRunners(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	ids, err := a.service.Runners.ProjectRunners(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, projectRunnersRequest{RunnerIDs: ids})
}
func (a *api) setProjectRunners(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	var req projectRunnersRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	for _, id := range req.RunnerIDs {
		if !validUUID(id) {
			writeError(w, 400, "invalid_id", "runnerIds must contain UUIDs")
			return
		}
	}
	if err := a.service.Runners.SetProjectRunners(r.Context(), id, req.RunnerIDs); err != nil {
		writeAppError(w, err)
		return
	}
	if req.RunnerIDs == nil {
		req.RunnerIDs = []string{}
	}
	writeJSON(w, 200, req)
}
func (a *api) listRunners(w http.ResponseWriter, r *http.Request) {
	values, err := a.service.Runners.List(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]RunnerDTO, 0, len(values))
	for _, v := range values {
		out = append(out, a.runnerDTO(v))
	}
	writeJSON(w, 200, out)
}
func (a *api) createRunnerRegistration(w http.ResponseWriter, r *http.Request) {
	token, err := a.service.Runners.CreateRegistration(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, runnerRegistrationResponse{Token: token})
}
func (a *api) registerRunner(w http.ResponseWriter, r *http.Request) {
	var req runnerRegistrationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, token, err := a.service.Runners.Register(r.Context(), req.Token, req.Name)
	if err != nil {
		writeAppError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, runnerCredentialResponse{Runner: a.runnerDTO(v), Token: token})
}
func (a *api) getRunner(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, err := a.service.Runners.Get(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, a.runnerDTO(v))
}
func (a *api) renameRunner(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var req runnerNameRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, err := a.service.Runners.Rename(r.Context(), id, req.Name)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, a.runnerDTO(v))
}
func (a *api) rotateRunner(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, token, err := a.service.Runners.Rotate(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, runnerCredentialResponse{Runner: a.runnerDTO(v), Token: token})
}
func (a *api) revokeRunner(w http.ResponseWriter, r *http.Request) { a.disableRunner(w, r, false) }
func (a *api) deleteRunner(w http.ResponseWriter, r *http.Request) { a.disableRunner(w, r, true) }
func (a *api) disableRunner(w http.ResponseWriter, r *http.Request, deleted bool) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, err := a.service.Runners.Revoke(r.Context(), id, deleted)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if deleted {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, 200, a.runnerDTO(v))
}
