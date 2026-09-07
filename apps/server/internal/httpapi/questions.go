package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type QuestionDTO struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"projectId"`
	IssueID        string          `json:"issueId"`
	RunID          string          `json:"runId"`
	Prompt         string          `json:"prompt"`
	Kind           string          `json:"kind"`
	Options        json.RawMessage `json:"options"`
	Recommendation *string         `json:"recommendation,omitempty"`
	Blocking       bool            `json:"blocking"`
	Status         string          `json:"status"`
	CreatedAt      time.Time       `json:"createdAt"`
	AnsweredAt     *time.Time      `json:"answeredAt,omitempty"`
}

type QuestionAnswerRequest struct {
	Kind      string   `json:"kind"`
	Text      *string  `json:"text,omitempty"`
	OptionIDs []string `json:"optionIds,omitempty"`
}

type QuestionAnswerResponse struct {
	Question   QuestionDTO `json:"question"`
	DecisionID string      `json:"decisionId"`
	Run        RunDTO      `json:"run"`
	ResumeJobID *string    `json:"resumeJobId,omitempty"`
}

func (a *api) registerQuestionRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/questions", a.listQuestions)
	r.Get("/projects/{projectID}/questions/{questionID}", a.getQuestion)
	r.Post("/projects/{projectID}/questions/{questionID}/answer", a.answerQuestion)
}

func (a *api) listQuestions(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	filter := store.QuestionFilter{}
	if issueID := r.URL.Query().Get("issueId"); issueID != "" {
		if !validUUID(issueID) {
			writeError(w, http.StatusBadRequest, "invalid_id", "issueId must be a UUID")
			return
		}
		filter.IssueID = &issueID
	}
	if runID := r.URL.Query().Get("runId"); runID != "" {
		if !validUUID(runID) {
			writeError(w, http.StatusBadRequest, "invalid_id", "runId must be a UUID")
			return
		}
		filter.RunID = &runID
	}
	if status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))); status != "" {
		switch status {
		case "OPEN", "ANSWERED", "CANCELLED":
			filter.Statuses = []string{status}
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", "status must be OPEN, ANSWERED or CANCELLED")
			return
		}
	}
	if _, err := a.service.GetProject(r.Context(), projectID); err != nil {
		writeAppError(w, err)
		return
	}
	questions, err := a.questions.List(r.Context(), projectID, filter)
	if err != nil {
		writeAppError(w, err)
		return
	}
	response := make([]QuestionDTO, 0, len(questions))
	for _, question := range questions {
		response = append(response, questionDTO(question))
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *api) getQuestion(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	questionID, ok := pathUUID(w, r, "questionID")
	if !ok {
		return
	}
	question, err := a.questions.Get(r.Context(), projectID, questionID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, questionDTO(question))
}

func (a *api) answerQuestion(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	questionID, ok := pathUUID(w, r, "questionID")
	if !ok {
		return
	}
	var request QuestionAnswerRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.questions.Answer(r.Context(), projectID, questionID, store.QuestionAnswer{
		Kind:      request.Kind,
		Text:      request.Text,
		OptionIDs: append([]string(nil), request.OptionIDs...),
	}, nil)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var resumeJobID *string
	if result.Job != nil {
		id := result.Job.ID
		resumeJobID = &id
	}
	writeJSON(w, http.StatusOK, QuestionAnswerResponse{
		Question:    questionDTO(result.Question),
		DecisionID:  result.Decision.ID,
		Run:         runDTO(result.Run),
		ResumeJobID: resumeJobID,
	})
}

func questionDTO(question store.Question) QuestionDTO {
	options := append(json.RawMessage(nil), question.Options...)
	if len(options) == 0 {
		options = json.RawMessage(`[]`)
	}
	return QuestionDTO{
		ID:             question.ID,
		ProjectID:      question.ProjectID,
		IssueID:        question.IssueID,
		RunID:          question.RunID,
		Prompt:         question.Prompt,
		Kind:           question.Kind,
		Options:        options,
		Recommendation: question.Recommendation,
		Blocking:       question.Blocking,
		Status:         question.Status,
		CreatedAt:      question.CreatedAt,
		AnsweredAt:     question.AnsweredAt,
	}
}
