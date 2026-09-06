package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/go-chi/chi/v5"
)

type api struct {
	service               *app.Service
	questions             *app.QuestionService
	runEvidence           *app.RunEvidenceService
	secrets               app.SecretWriter
	secretWriteAuthorizer SecretWriteAuthorizer
}

type healthResponse struct {
	Status string `json:"status"`
}

func NewRouter(services ...*app.Service) http.Handler {
	var service *app.Service
	if len(services) > 0 {
		service = services[0]
	}
	return newRouter(service, nil, nil, nil)
}

func NewRouterWithSecrets(service *app.Service, secrets app.SecretWriter, authorizers ...SecretWriteAuthorizer) http.Handler {
	var authorizer SecretWriteAuthorizer
	if len(authorizers) > 0 {
		authorizer = authorizers[0]
	}
	return newRouter(service, nil, secrets, authorizer)
}

func NewRouterWithApplication(services *app.Services, authorizers ...SecretWriteAuthorizer) http.Handler {
	if services == nil {
		return newRouter(nil, nil, nil, nil)
	}
	var authorizer SecretWriteAuthorizer
	if len(authorizers) > 0 {
		authorizer = authorizers[0]
	}
	return newRouter(services.ControlPlane, services.RunEvidence, services.Secrets, authorizer, services.Questions)
}

func newRouter(service *app.Service, runEvidence *app.RunEvidenceService, secretWriter app.SecretWriter, secretWriteAuthorizer SecretWriteAuthorizer, questionServices ...*app.QuestionService) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", handleHealth)
	if service == nil {
		return router
	}
	var questions *app.QuestionService
	if len(questionServices) > 0 {
		questions = questionServices[0]
	}
	a := &api{service: service, questions: questions, runEvidence: runEvidence, secrets: secretWriter, secretWriteAuthorizer: secretWriteAuthorizer}
	router.Route("/api", func(r chi.Router) {
		a.registerConfigurationRoutes(r)
		a.registerIssueRunRoutes(r)
		a.registerRunEvidenceRoutes(r)
		if a.questions != nil {
			a.registerQuestionRoutes(r)
		}
		if a.secrets != nil {
			a.registerSecretRoutes(r)
		}
	})
	return router
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return false
	}
	if trimmed[0] != '{' {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return false
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return false
	}
	return true
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := chi.URLParam(r, name)
	if !validUUID(value) {
		writeError(w, http.StatusBadRequest, "invalid_id", name+" must be a UUID")
		return "", false
	}
	return value, true
}

func writeAppError(w http.ResponseWriter, err error) {
	if apiErr, ok := app.AsError(err); ok {
		status := http.StatusBadRequest
		switch {
		case strings.HasSuffix(apiErr.Code, "_not_found"):
			status = http.StatusNotFound
		case apiErr.Code == "conflict", apiErr.Code == "issue_done", apiErr.Code == "agent_unavailable":
			status = http.StatusConflict
		case apiErr.Code == "execution_configuration_invalid":
			status = http.StatusUnprocessableEntity
		}
		writeError(w, status, apiErr.Code, apiErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
