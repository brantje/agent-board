package httpapi

import (
	"net/http"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type squadMemberRequest struct {
	AgentID string  `json:"agentId"`
	Role    *string `json:"role"`
}

type squadRequest struct {
	Name          string               `json:"name"`
	LeaderAgentID string               `json:"leaderAgentId"`
	Members       []squadMemberRequest `json:"members"`
}

type squadMemberResponse struct {
	AgentID string  `json:"agentId"`
	Role    *string `json:"role"`
}

type squadResponse struct {
	ID            string                `json:"id"`
	ProjectID     string                `json:"projectId"`
	Name          string                `json:"name"`
	LeaderAgentID string                `json:"leaderAgentId"`
	Members       []squadMemberResponse `json:"members"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
}

func (a *api) registerSquadRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/squads", a.listSquads)
	r.Post("/projects/{projectID}/squads", a.createSquad)
	r.Get("/projects/{projectID}/squads/{squadID}", a.getSquad)
	r.Put("/projects/{projectID}/squads/{squadID}", a.updateSquad)
	r.Delete("/projects/{projectID}/squads/{squadID}", a.deleteSquad)
}

func squadStoreInput(projectID, squadID string, req squadRequest) store.Squad {
	members := make([]store.SquadMember, 0, len(req.Members))
	for _, member := range req.Members {
		members = append(members, store.SquadMember{AgentID: member.AgentID, Role: member.Role})
	}
	return store.Squad{
		ID:            squadID,
		ProjectID:     projectID,
		Name:          req.Name,
		LeaderAgentID: req.LeaderAgentID,
		Members:       members,
	}
}

func squadHTTPResponse(value store.Squad) squadResponse {
	members := make([]squadMemberResponse, 0, len(value.Members))
	for _, member := range value.Members {
		members = append(members, squadMemberResponse{AgentID: member.AgentID, Role: member.Role})
	}
	return squadResponse{
		ID:            value.ID,
		ProjectID:     value.ProjectID,
		Name:          value.Name,
		LeaderAgentID: value.LeaderAgentID,
		Members:       members,
		CreatedAt:     value.CreatedAt,
		UpdatedAt:     value.UpdatedAt,
	}
}

func (a *api) listSquads(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	values, err := a.projectAccess.ListSquads(r.Context(), actor, projectID)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]squadResponse, 0, len(values))
	for _, value := range values {
		out = append(out, squadHTTPResponse(value))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) getSquad(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	squadID, ok := pathUUID(w, r, "squadID")
	if !ok {
		return
	}
	value, err := a.projectAccess.GetSquad(r.Context(), actor, projectID, squadID)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, squadHTTPResponse(value))
}

func (a *api) createSquad(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	var req squadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	value, err := a.projectAccess.CreateSquad(r.Context(), actor, squadStoreInput(projectID, "", req))
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, squadHTTPResponse(value))
}

func (a *api) updateSquad(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	squadID, ok := pathUUID(w, r, "squadID")
	if !ok {
		return
	}
	var req squadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	value, err := a.projectAccess.UpdateSquad(r.Context(), actor, squadStoreInput(projectID, squadID, req))
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, squadHTTPResponse(value))
}

func (a *api) deleteSquad(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	squadID, ok := pathUUID(w, r, "squadID")
	if !ok {
		return
	}
	if err := a.projectAccess.DeleteSquad(r.Context(), actor, projectID, squadID); err != nil {
		writeProjectAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
