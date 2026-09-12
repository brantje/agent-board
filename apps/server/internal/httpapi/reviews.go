package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type ReviewDTO struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"projectId"`
	IssueID        string     `json:"issueId"`
	RunID          string     `json:"runId"`
	Status         string     `json:"status"`
	BaseRevision   string     `json:"baseRevision"`
	ReviewRevision string     `json:"reviewRevision"`
	RequestedAt    time.Time  `json:"requestedAt"`
	DecidedAt      *time.Time `json:"decidedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ReviewDecisionDTO struct {
	ID          string          `json:"id"`
	Outcome     string          `json:"outcome"`
	ActorType   string          `json:"actorType"`
	ActorID     *string         `json:"actorId"`
	SafeDetails json.RawMessage `json:"safeDetails"`
	CreatedAt   time.Time       `json:"createdAt"`
}

type ReviewDetailDTO struct {
	Review     ReviewDTO            `json:"review"`
	Decision   *ReviewDecisionDTO   `json:"decision"`
	TestStatus app.ReviewTestStatus `json:"testStatus"`
	Evidence   RunEvidenceDTO       `json:"evidence"`
}

type ReviewApprovalResponse struct {
	Review   ReviewDTO         `json:"review"`
	Decision ReviewDecisionDTO `json:"decision"`
	Run      RunDTO            `json:"run"`
	Issue    IssueDTO          `json:"issue"`
}

type ReviewRequestChangesRequest struct {
	Feedback string `json:"feedback"`
}

type ReviewRequestChangesResponse struct {
	Review   ReviewDTO         `json:"review"`
	Decision ReviewDecisionDTO `json:"decision"`
	Run      RunDTO            `json:"run"`
	Issue    IssueDTO          `json:"issue"`
	JobID    string            `json:"jobId"`
}

func (a *api) registerReviewRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/reviews", a.listReviews)
	r.Get("/projects/{projectID}/reviews/{reviewID}", a.getReview)
	r.Post("/projects/{projectID}/reviews/{reviewID}/approve", a.approveReview)
	r.Post("/projects/{projectID}/reviews/{reviewID}/request-changes", a.requestReviewChanges)
}

func (a *api) listReviews(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	if _, err := a.service.GetProject(r.Context(), projectID); err != nil {
		writeAppError(w, err)
		return
	}
	filter := store.ReviewFilter{}
	issueUUID, ok := queryIssueKey(w, r, "issueId", projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	filter.IssueID = issueUUID
	if status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))); status != "" {
		switch status {
		case "PENDING", "APPROVED", "CHANGES_REQUESTED", "CANCELLED":
			filter.Statuses = []string{status}
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", "status must be PENDING, APPROVED, CHANGES_REQUESTED or CANCELLED")
			return
		}
	}
	values, err := a.reviews.List(r.Context(), projectID, filter)
	if err != nil {
		writeAppError(w, err)
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	response := make([]ReviewDTO, 0, len(values))
	for _, value := range values {
		response = append(response, reviewDTO(value, keys))
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *api) getReview(w http.ResponseWriter, r *http.Request) {
	projectID, reviewID, ok := reviewPath(w, r)
	if !ok {
		return
	}
	value, err := a.reviews.Get(r.Context(), projectID, reviewID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	response := ReviewDetailDTO{
		Review:     reviewDTO(value.Review, keys),
		TestStatus: value.TestStatus,
		Evidence:   runEvidenceDTO(value.Evidence, keys),
	}
	if value.Decision != nil {
		decision := reviewDecisionDTO(*value.Decision)
		response.Decision = &decision
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *api) approveReview(w http.ResponseWriter, r *http.Request) {
	projectID, reviewID, ok := reviewPath(w, r)
	if !ok {
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	result, err := a.reviews.Approve(r.Context(), projectID, reviewID, nil)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ReviewApprovalResponse{
		Review:   reviewDTO(result.Review, keys),
		Decision: reviewDecisionDTO(result.Decision),
		Run:      runDTO(result.Run, keys),
		Issue:    issueDTO(result.Issue),
	})
}

func (a *api) requestReviewChanges(w http.ResponseWriter, r *http.Request) {
	projectID, reviewID, ok := reviewPath(w, r)
	if !ok {
		return
	}
	var request ReviewRequestChangesRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	keys, err := a.issueKeyMap(r.Context(), projectID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	result, err := a.reviews.RequestChanges(r.Context(), projectID, reviewID, request.Feedback, nil)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, ReviewRequestChangesResponse{
		Review:   reviewDTO(result.Review, keys),
		Decision: reviewDecisionDTO(result.Decision),
		Run:      runDTO(result.Run, keys),
		Issue:    issueDTO(result.Issue),
		JobID:    result.Job.ID,
	})
}

func reviewPath(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return "", "", false
	}
	reviewID, ok := pathUUID(w, r, "reviewID")
	if !ok {
		return "", "", false
	}
	return projectID, reviewID, true
}

func reviewDTO(value store.Review, issueKeys map[string]string) ReviewDTO {
	return ReviewDTO{
		ID: value.ID, ProjectID: value.ProjectID, IssueID: issueKeyForUUID(issueKeys, value.IssueID), RunID: value.RunID,
		Status: value.Status, BaseRevision: value.BaseRevision, ReviewRevision: value.ReviewRevision,
		RequestedAt: value.RequestedAt, DecidedAt: value.DecidedAt,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func reviewDecisionDTO(value store.Decision) ReviewDecisionDTO {
	details := append(json.RawMessage(nil), value.SafeDetails...)
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	return ReviewDecisionDTO{
		ID: value.ID, Outcome: value.Outcome, ActorType: value.ActorType, ActorID: value.ActorID,
		SafeDetails: details, CreatedAt: value.CreatedAt,
	}
}
