package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func validIssuePriority(priority int) bool {
	return priority >= 0 && priority <= 4
}

func (a *api) registerIssueRunRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/issues", a.listIssues)
	r.Post("/projects/{projectID}/issues", a.createIssue)
	r.Get("/projects/{projectID}/issues/{issueID}", a.getIssue)
	r.Patch("/projects/{projectID}/issues/{issueID}", a.updateIssue)
	r.Get("/projects/{projectID}/issues/{issueID}/relationships", a.listIssueRelationships)
	r.Post("/projects/{projectID}/issues/{issueID}/relationships", a.createIssueRelationship)
	r.Delete("/projects/{projectID}/issues/{issueID}/relationships/{relationshipID}", a.deleteIssueRelationship)
	r.Post("/projects/{projectID}/issues/{issueID}/assignment", a.assignIssue)
	r.Get("/projects/{projectID}/issues/{issueID}/execution", a.getIssueExecutionState)
	r.Post("/projects/{projectID}/issues/{issueID}/runs", a.startIssueRun)
	r.Get("/projects/{projectID}/assignees", a.listIssueAssignees)
	r.Get("/projects/{projectID}/runs", a.listRuns)
	r.Get("/projects/{projectID}/runs/{runID}", a.getRun)
	if a.eventHub != nil {
		r.Get("/projects/{projectID}/events", a.streamProjectEvents)
	}
}

func (a *api) listIssues(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	values, err := a.service.ListIssues(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]IssueDTO, 0, len(values))
	for _, value := range values {
		out = append(out, issueDTO(value))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) createIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	var req CreateIssueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	priority := 0
	if req.Priority != nil {
		priority = *req.Priority
	}
	if !validIssuePriority(priority) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "priority must be between 0 and 4")
		return
	}
	input := store.Issue{ProjectID: projectID, Title: req.Title, Description: req.Description, Status: req.Status, Priority: priority}
	var value store.Issue
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		value, err = a.projectAccess.CreateIssue(r.Context(), actor, input)
	} else {
		value, err = a.service.CreateIssue(r.Context(), input)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueDTO(value))
}

func (a *api) getIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	value, err := a.service.GetIssue(r.Context(), projectID, issueUUID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueDTO(value))
}

func (a *api) updateIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req UpdateIssueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Priority != nil && !validIssuePriority(*req.Priority) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "priority must be between 0 and 4")
		return
	}
	patch := store.IssuePatch{
		ProjectID:   projectID,
		ID:          issueUUID,
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		Priority:    req.Priority,
	}
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		value, err := a.projectAccess.PatchIssue(r.Context(), actor, patch)
		if err != nil {
			writeProjectAccessError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, issueDTO(value))
		return
	}
	value, err := a.service.PatchIssue(r.Context(), patch)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueDTO(value))
}

func (a *api) listIssueRelationships(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var values []store.IssueRelationship
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		values, err = a.projectAccess.ListIssueRelationships(r.Context(), actor, projectID, issueUUID)
	} else {
		values, err = a.service.ListIssueRelationships(r.Context(), projectID, issueUUID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]IssueRelationshipDTO, 0, len(values))
	for _, value := range values {
		out = append(out, issueRelationshipDTO(value, keys))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) createIssueRelationship(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req CreateIssueRelationshipRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	targetUUID, ok := bodyIssueKey(w, r, projectID, req.TargetIssueID, "targetIssueId", a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	input := store.IssueRelationship{ProjectID: projectID, SourceIssueID: issueUUID, TargetIssueID: targetUUID, Type: req.Type}
	var value store.IssueRelationship
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		value, err = a.projectAccess.CreateIssueRelationship(r.Context(), actor, input)
	} else {
		value, err = a.service.CreateIssueRelationship(r.Context(), input)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	keys := issueKeysFromResolved(issueUUID, chi.URLParam(r, "issueID"), targetUUID, req.TargetIssueID)
	writeJSON(w, http.StatusCreated, issueRelationshipDTO(value, keys))
}

func (a *api) deleteIssueRelationship(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	relationshipID, ok := pathUUID(w, r, "relationshipID")
	if !ok {
		return
	}
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		err = a.projectAccess.DeleteIssueRelationship(r.Context(), actor, projectID, issueUUID, relationshipID)
	} else {
		err = a.service.DeleteIssueRelationship(r.Context(), projectID, issueUUID, relationshipID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listRuns(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	values, err := a.service.ListRuns(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]RunDTO, 0, len(values))
	for _, value := range values {
		out = append(out, runDTO(value, keys))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) getRun(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	runID, ok := pathUUID(w, r, "runID")
	if !ok {
		return
	}
	value, err := a.service.GetRun(r.Context(), projectID, runID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runDTO(value, keys))
}

func (a *api) assignIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req AssignmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.AssignedTo) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "assignedTo is required; use null to unassign")
		return
	}
	var input *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(req.AssignedTo))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "invalid assignedTo")
		return
	}
	var target *store.Assignee
	if input != nil {
		target = &store.Assignee{Type: input.Type, ID: input.ID}
	}
	var issue store.Issue
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		issue, err = a.projectAccess.SetIssueAssignee(r.Context(), actor, projectID, issueUUID, target)
	} else {
		issue, err = a.service.SetIssueAssignee(r.Context(), projectID, issueUUID, target, store.EmptyObject)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AssignmentResponse{Issue: issueDTO(issue)})
}

func (a *api) listIssueAssignees(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	var result []store.Assignee
	var err error
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		result, err = a.projectAccess.ListIssueAssignees(r.Context(), actor, projectID)
	} else {
		result, err = a.service.ListIssueAssignees(r.Context(), projectID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
