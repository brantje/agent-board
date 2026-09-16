package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type IssuePlacementRequest struct {
	Status   *string                `json:"status"`
	BeforeID optionalNullableString `json:"beforeId"`
	AfterID  optionalNullableString `json:"afterId"`
}

func (a *api) registerBoardPlacementRoutes(r chi.Router) {
	r.Post("/projects/{projectID}/issues/{issueID}/placement", a.placeIssue)
}

func (a *api) placeIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req IssuePlacementRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !req.BeforeID.Set || !req.AfterID.Set {
		writeError(w, http.StatusBadRequest, "invalid_argument", "beforeId and afterId must both be provided")
		return
	}
	beforeID, ok := a.resolvePlacementAnchor(w, r, projectID, req.BeforeID.Value, "beforeId")
	if !ok {
		return
	}
	afterID, ok := a.resolvePlacementAnchor(w, r, projectID, req.AfterID.Value, "afterId")
	if !ok {
		return
	}
	input := store.IssuePlacement{
		ProjectID: projectID,
		IssueID:   issueUUID,
		Status:    req.Status,
		BeforeID:  beforeID,
		AfterID:   afterID,
	}
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		value, err := a.projectAccess.PlaceIssue(r.Context(), actor, input)
		if err != nil {
			writeProjectAccessError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, issueDTO(value))
		return
	}
	value, err := a.service.PlaceIssue(r.Context(), input)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueDTO(value))
}

func (a *api) resolvePlacementAnchor(w http.ResponseWriter, r *http.Request, projectID string, value *string, field string) (*string, bool) {
	if value == nil {
		return nil, true
	}
	issueUUID, ok := bodyIssueKey(w, r, projectID, *value, field, a.service.ResolveIssueUUID)
	if !ok {
		return nil, false
	}
	return &issueUUID, true
}
