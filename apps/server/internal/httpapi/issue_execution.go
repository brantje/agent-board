package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func (a *api) startIssueRun(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	// No caller-selected Agent, ownership or status is accepted.
	if r.ContentLength != 0 {
		var input struct{}
		if !decodeJSON(w, r, &input) {
			return
		}
	}
	var run store.Run
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		run, err = a.projectAccess.StartIssueRun(r.Context(), actor, projectID, issueID)
	} else {
		run, err = a.service.StartIssueRun(r.Context(), projectID, issueID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runDTO(run, map[string]string{issueID: chi.URLParam(r, "issueID")}))
}
