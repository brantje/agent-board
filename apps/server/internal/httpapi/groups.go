package httpapi

import (
	"net/http"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type groupRequest struct {
	Name string `json:"name"`
}

type groupMemberRequest struct {
	UserID string `json:"userID"`
}

type groupResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func groupDTO(group store.Group) groupResponse {
	return groupResponse{
		ID:        group.ID,
		Name:      group.Name,
		CreatedAt: group.CreatedAt,
		UpdatedAt: group.UpdatedAt,
	}
}

func (a *api) registerGroupRoutes(r chi.Router) {
	r.Get("/groups", a.handleListGroups)
	r.Post("/groups", a.handleCreateGroup)
	r.Patch("/groups/{groupID}", a.handleUpdateGroup)
	r.Delete("/groups/{groupID}", a.handleDeleteGroup)
	r.Get("/groups/{groupID}/members", a.handleListGroupMembers)
	r.Post("/groups/{groupID}/members", a.handleAddGroupMember)
	r.Delete("/groups/{groupID}/members/{userID}", a.handleRemoveGroupMember)
}

func (a *api) handleListGroups(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	groups, err := a.auth.ListGroups(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	response := make([]groupResponse, 0, len(groups))
	for _, group := range groups {
		response = append(response, groupDTO(group))
	}
	writeSensitiveJSON(w, http.StatusOK, response)
}

func (a *api) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	var input groupRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	group, err := a.auth.CreateGroup(r.Context(), actor, input.Name)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, groupDTO(group))
}

func (a *api) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	groupID, ok := pathUUID(w, r, "groupID")
	if !ok {
		return
	}
	var input groupRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	group, err := a.auth.UpdateGroup(r.Context(), actor, groupID, input.Name)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groupDTO(group))
}

func (a *api) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	groupID, ok := pathUUID(w, r, "groupID")
	if !ok {
		return
	}
	if err := a.auth.DeleteGroup(r.Context(), actor, groupID); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	groupID, ok := pathUUID(w, r, "groupID")
	if !ok {
		return
	}
	members, err := a.auth.ListGroupMembers(r.Context(), actor, groupID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	response := make([]authUserResponse, 0, len(members))
	for _, member := range members {
		response = append(response, authUserDTO(member))
	}
	writeSensitiveJSON(w, http.StatusOK, response)
}

func (a *api) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	groupID, ok := pathUUID(w, r, "groupID")
	if !ok {
		return
	}
	var input groupMemberRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if !validUUID(input.UserID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "userID must be a UUID")
		return
	}
	if err := a.auth.AddGroupMember(r.Context(), actor, groupID, input.UserID); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	groupID, ok := pathUUID(w, r, "groupID")
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "userID")
	if !ok {
		return
	}
	if err := a.auth.RemoveGroupMember(r.Context(), actor, groupID, userID); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
