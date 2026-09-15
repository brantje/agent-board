package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (a *api) placeIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req PlaceIssueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var beforeUUID *string
	if req.BeforeIssueID != nil {
		resolved, ok := bodyIssueKey(w, r, projectID, *req.BeforeIssueID, "beforeIssueId", a.service.ResolveIssueUUID)
		if !ok {
			return
		}
		beforeUUID = &resolved
	}
	input := store.IssueBoardPlacement{ProjectID: projectID, IssueID: issueUUID, Status: req.Status, BeforeIssueID: beforeUUID}
	var value store.Issue
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		value, err = a.projectAccess.PlaceIssue(r.Context(), actor, input)
	} else {
		value, err = a.service.PlaceIssue(r.Context(), input)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueDTO(value))
}
