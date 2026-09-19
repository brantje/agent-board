package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func (a *api) listIssueComments(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueKey := chi.URLParam(r, "issueID")
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}

	var values []store.IssueComment
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		values, err = a.projectAccess.ListIssueComments(r.Context(), actor, projectID, issueUUID)
	} else {
		values, err = a.service.ListIssueComments(r.Context(), projectID, issueUUID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]IssueCommentDTO, 0, len(values))
	for _, value := range values {
		out = append(out, issueCommentDTO(value, issueKey))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) createIssueComment(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueKey := chi.URLParam(r, "issueID")
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req CreateIssueCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ParentCommentID != nil && !validUUID(*req.ParentCommentID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "parentCommentId must be a UUID")
		return
	}
	if a.projectAccess == nil {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return
	}
	actor, ok := projectActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return
	}
	value, err := a.projectAccess.CreateIssueComment(r.Context(), actor, app.CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueUUID, ParentCommentID: req.ParentCommentID, Body: req.Body,
	})
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueCommentDTO(value, issueKey))
}

func (a *api) listIssueTimeline(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueKey := chi.URLParam(r, "issueID")
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}

	var values []app.IssueTimelineEntry
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		values, err = a.projectAccess.ListIssueTimeline(r.Context(), actor, projectID, issueUUID)
	} else {
		values, err = a.service.ListIssueTimeline(r.Context(), projectID, issueUUID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]IssueTimelineEntryDTO, 0, len(values))
	for _, value := range values {
		out = append(out, issueTimelineEntryDTO(value, issueKey))
	}
	writeJSON(w, http.StatusOK, out)
}
