package httpapi

import (
	"net/http"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type squadMemberDTO struct {
	AgentID string  `json:"agentId"`
	Role    *string `json:"role,omitempty"`
}

type squadDTO struct {
	ID            string           `json:"id"`
	ProjectID     string           `json:"projectId"`
	Name          string           `json:"name"`
	LeaderAgentID string           `json:"leaderAgentId"`
	Members       []squadMemberDTO `json:"members"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

type squadInput struct {
	Name          string           `json:"name"`
	LeaderAgentID string           `json:"leaderAgentId"`
	Members       []squadMemberDTO `json:"members"`
}

func (a *api) registerSquadRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/squads", a.listSquads)
	r.Post("/projects/{projectID}/squads", a.createSquad)
	r.Get("/projects/{projectID}/squads/{squadID}", a.getSquad)
	r.Put("/projects/{projectID}/squads/{squadID}", a.updateSquad)
	r.Delete("/projects/{projectID}/squads/{squadID}", a.deleteSquad)
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
	out := make([]squadDTO, 0, len(values))
	for _, value := range values {
		out = append(out, squadResponse(value))
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
	writeJSON(w, http.StatusOK, squadResponse(value))
}

func (a *api) createSquad(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	input, ok := decodeSquadInput(w, r)
	if !ok {
		return
	}
	value, err := a.projectAccess.CreateSquad(r.Context(), actor, store.Squad{
		ProjectID:     projectID,
		Name:          input.Name,
		LeaderAgentID: input.LeaderAgentID,
		Members:       squadMembers(input.Members),
	})
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, squadResponse(value))
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
	input, ok := decodeSquadInput(w, r)
	if !ok {
		return
	}
	value, err := a.projectAccess.UpdateSquad(r.Context(), actor, store.Squad{
		ID:            squadID,
		ProjectID:     projectID,
		Name:          input.Name,
		LeaderAgentID: input.LeaderAgentID,
		Members:       squadMembers(input.Members),
	})
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, squadResponse(value))
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

func decodeSquadInput(w http.ResponseWriter, r *http.Request) (squadInput, bool) {
	var input squadInput
	if !decodeJSON(w, r, &input) {
		return squadInput{}, false
	}
	if input.Members == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "members is required")
		return squadInput{}, false
	}
	if !validUUID(input.LeaderAgentID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "leaderAgentId must be a UUID")
		return squadInput{}, false
	}
	for _, member := range input.Members {
		if !validUUID(member.AgentID) {
			writeError(w, http.StatusBadRequest, "invalid_id", "members[].agentId must be a UUID")
			return squadInput{}, false
		}
	}
	return input, true
}

func squadMembers(values []squadMemberDTO) []store.SquadMember {
	members := make([]store.SquadMember, 0, len(values))
	for _, value := range values {
		members = append(members, store.SquadMember{AgentID: value.AgentID, Role: value.Role})
	}
	return members
}

func squadResponse(value store.Squad) squadDTO {
	members := make([]squadMemberDTO, 0, len(value.Members))
	for _, member := range value.Members {
		members = append(members, squadMemberDTO{AgentID: member.AgentID, Role: member.Role})
	}
	return squadDTO{
		ID:            value.ID,
		ProjectID:     value.ProjectID,
		Name:          value.Name,
		LeaderAgentID: value.LeaderAgentID,
		Members:       members,
		CreatedAt:     value.CreatedAt,
		UpdatedAt:     value.UpdatedAt,
	}
}
