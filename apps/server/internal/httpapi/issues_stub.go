package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func (a *api) registerIssueRunRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/issues", a.listIssues)
	r.Post("/projects/{projectID}/issues", a.createIssue)
	r.Get("/projects/{projectID}/issues/{issueID}", a.getIssue)
	r.Patch("/projects/{projectID}/issues/{issueID}", a.updateIssue)
	r.Get("/projects/{projectID}/issues/{issueID}/relationships", a.listIssueRelationships)
	r.Post("/projects/{projectID}/issues/{issueID}/relationships", a.createIssueRelationship)
	r.Delete("/projects/{projectID}/issues/{issueID}/relationships/{relationshipID}", a.deleteIssueRelationship)
	r.Post("/projects/{projectID}/issues/{issueID}/assignment", a.assignIssue)
	r.Get("/projects/{projectID}/runs", a.listRuns)
	r.Get("/projects/{projectID}/runs/{runID}", a.getRun)
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
	status := req.Status
	if status == "" {
		status = "BACKLOG"
	}
	priority := 0
	if req.Priority != nil {
		priority = *req.Priority
	}
	value, err := a.service.CreateIssue(r.Context(), store.Issue{
		ProjectID:   projectID,
		Title:       req.Title,
		Description: req.Description,
		Status:      status,
		Priority:    priority,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueDTO(value))
}

func (a *api) getIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueID, ok := pathUUID(w, r, "issueID")
	if !ok {
		return
	}
	value, err := a.service.GetIssue(r.Context(), projectID, issueID)
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
	issueID, ok := pathUUID(w, r, "issueID")
	if !ok {
		return
	}
	current, err := a.service.GetIssue(r.Context(), projectID, issueID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req UpdateIssueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Title != nil {
		current.Title = *req.Title
	}
	if req.Description != nil {
		current.Description = *req.Description
	}
	if req.Status != nil {
		current.Status = *req.Status
	}
	if req.Priority != nil {
		current.Priority = *req.Priority
	}
	value, err := a.service.UpdateIssue(r.Context(), current)
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
	issueID, ok := pathUUID(w, r, "issueID")
	if !ok {
		return
	}
	values, err := a.service.ListIssueRelationships(r.Context(), projectID, issueID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]IssueRelationshipDTO, 0, len(values))
	for _, value := range values {
		out = append(out, issueRelationshipDTO(value))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) createIssueRelationship(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueID, ok := pathUUID(w, r, "issueID")
	if !ok {
		return
	}
	var req CreateIssueRelationshipRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validUUID(req.TargetIssueID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "targetIssueId must be a UUID")
		return
	}
	value, err := a.service.CreateIssueRelationship(r.Context(), store.IssueRelationship{
		ProjectID:     projectID,
		SourceIssueID: issueID,
		TargetIssueID: req.TargetIssueID,
		Type:          req.Type,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueRelationshipDTO(value))
}

func (a *api) deleteIssueRelationship(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueID, ok := pathUUID(w, r, "issueID")
	if !ok {
		return
	}
	relationshipID, ok := pathUUID(w, r, "relationshipID")
	if !ok {
		return
	}
	if err := a.service.DeleteIssueRelationship(r.Context(), projectID, issueID, relationshipID); err != nil {
		writeAppError(w, err)
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
	out := make([]RunDTO, 0, len(values))
	for _, value := range values {
		out = append(out, runDTO(value))
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
	writeJSON(w, http.StatusOK, runDTO(value))
}

func (a *api) assignIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueID, ok := pathUUID(w, r, "issueID")
	if !ok {
		return
	}
	var req AssignmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validUUID(req.AgentID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "agentId must be a UUID")
		return
	}
	issue, run, err := a.service.AssignIssue(r.Context(), projectID, issueID, req.AgentID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, AssignmentResponse{Issue: issueDTO(issue), Run: runDTO(run)})
}
