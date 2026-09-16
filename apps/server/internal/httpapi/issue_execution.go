package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type IssueExecutionAgentDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type IssueExecutionStateDTO struct {
	State          string                  `json:"state"`
	CanStart       bool                    `json:"canStart"`
	ExecutionAgent *IssueExecutionAgentDTO `json:"executionAgent"`
	ActiveRun      *RunDTO                 `json:"activeRun"`
}

func (a *api) getIssueExecutionState(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var state store.IssueExecutionState
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		state, err = a.projectAccess.GetIssueExecutionState(r.Context(), actor, projectID, issueID)
	} else {
		state, err = a.service.GetIssueExecutionState(r.Context(), projectID, issueID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := IssueExecutionStateDTO{State: state.State, CanStart: state.CanStart}
	if state.ExecutionAgent != nil {
		out.ExecutionAgent = &IssueExecutionAgentDTO{ID: state.ExecutionAgent.ID, Name: state.ExecutionAgent.Name}
	}
	if state.ActiveRun != nil {
		run := runDTO(*state.ActiveRun, issueKeysFromPath(issueID, chi.URLParam(r, "issueID")))
		out.ActiveRun = &run
	}
	writeJSON(w, http.StatusOK, out)
}

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
