package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	runnerconn "github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type RunnerDTO struct {
	ID                string          `json:"id"`
	Name              *string         `json:"name"`
	Internal          bool            `json:"internal"`
	Managed           bool            `json:"managed"`
	Deletable         bool            `json:"deletable"`
	Connected         bool            `json:"connected"`
	RegisteredAt      *time.Time      `json:"registeredAt"`
	RevokedAt         *time.Time      `json:"revokedAt"`
	LastSeenAt        *time.Time      `json:"lastSeenAt"`
	Capabilities      json.RawMessage `json:"capabilities"`
	ActiveSessions    *int            `json:"activeSessions"`
	ReservedSessions  int             `json:"reservedSessions"`
	MaxActiveSessions int             `json:"maxActiveSessions"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

func (a *api) runnerDTO(v store.Runner, reserved int) RunnerDTO {
	caps := v.Capabilities
	if len(caps) == 0 {
		caps = store.EmptyObject
	}
	var name *string
	if v.Name != "" {
		value := v.Name
		name = &value
	}
	dto := RunnerDTO{
		ID: v.ID, Name: name, Internal: v.Internal, Managed: v.Internal, Deletable: !v.Internal,
		Connected: a.service.Runners.Connections.Connected(v.ID), RegisteredAt: v.RegisteredAt,
		RevokedAt: v.RevokedAt, LastSeenAt: v.LastSeenAt, Capabilities: caps,
		ReservedSessions: reserved, MaxActiveSessions: runnerconn.MaxActiveSessions(v.Capabilities),
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
	if health, ok := a.service.Runners.Connections.Health(v.ID); ok {
		active := health.ActiveSessions
		dto.ActiveSessions = &active
	}
	return dto
}

type runnerUpdateRequest struct {
	Name              string `json:"name"`
	MaxActiveSessions *int   `json:"maxActiveSessions"`
}
type runnerRegistrationRequest struct {
	RegistrationToken string `json:"registrationToken"`
	Hostname          string `json:"hostname"`
}
type runnerIdentityDTO struct {
	ID string `json:"id"`
}
type runnerCreationResponse struct {
	Runner            runnerIdentityDTO `json:"runner"`
	RegistrationToken string            `json:"registrationToken"`
}
type runnerEnrollmentResponse struct {
	RunnerID    string `json:"runnerId"`
	RunnerToken string `json:"runnerToken"`
}
type runnerCredentialResponse struct {
	Runner      RunnerDTO `json:"runner"`
	RunnerToken string    `json:"runnerToken"`
}

func (a *api) registerRunnerRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/runners", a.getProjectRunners)
	r.Put("/projects/{projectID}/runners", a.setProjectRunners)
	r.Post("/runner/register", a.registerRunner)
	r.Handle("/runner/ws", a.service.Runners.Connections)
	r.Get("/runners", a.listRunners)
	r.Post("/runners", a.createRunner)
	r.Get("/runners/{resourceID}", a.getRunner)
	r.Patch("/runners/{resourceID}", a.updateRunner)
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
	ids := make([]string, len(values))
	for i, v := range values {
		ids[i] = v.ID
	}
	reserved, err := a.service.Runners.CountReservations(r.Context(), ids)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]RunnerDTO, 0, len(values))
	for _, v := range values {
		out = append(out, a.runnerDTO(v, reserved[v.ID]))
	}
	writeJSON(w, 200, out)
}
func (a *api) createRunner(w http.ResponseWriter, r *http.Request) {
	v, registrationToken, err := a.service.Runners.Create(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, runnerCreationResponse{Runner: runnerIdentityDTO{ID: v.ID}, RegistrationToken: registrationToken})
}
func (a *api) registerRunner(w http.ResponseWriter, r *http.Request) {
	var req runnerRegistrationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, runnerToken, err := a.service.Runners.Register(r.Context(), req.RegistrationToken, req.Hostname)
	if err != nil {
		writeAppError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, runnerEnrollmentResponse{RunnerID: v.ID, RunnerToken: runnerToken})
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
	reserved, err := a.service.Runners.CountReservations(r.Context(), []string{v.ID})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, a.runnerDTO(v, reserved[v.ID]))
}
func (a *api) updateRunner(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var req runnerUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, err := a.service.Runners.Update(r.Context(), id, req.Name, req.MaxActiveSessions)
	if err != nil {
		writeAppError(w, err)
		return
	}
	reserved, err := a.service.Runners.CountReservations(r.Context(), []string{v.ID})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, a.runnerDTO(v, reserved[v.ID]))
}
func (a *api) rotateRunner(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, runnerToken, err := a.service.Runners.Rotate(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reserved, err := a.service.Runners.CountReservations(r.Context(), []string{v.ID})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, runnerCredentialResponse{Runner: a.runnerDTO(v, reserved[v.ID]), RunnerToken: runnerToken})
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
	reserved, err := a.service.Runners.CountReservations(r.Context(), []string{v.ID})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, a.runnerDTO(v, reserved[v.ID]))
}
