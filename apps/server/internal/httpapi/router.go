package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/go-chi/chi/v5"
)

type api struct {
	service               *app.Service
	auth                  *app.AuthService
	projectAccess         *app.ProjectAccessService
	questions             *app.QuestionService
	reviews               *app.ReviewService
	runEvidence           *app.RunEvidenceService
	eventHub              *evidence.Hub
	secrets               app.SecretWriter
	secretResolver        executioncontext.SecretResolver
	secretWriteAuthorizer SecretWriteAuthorizer
	repositorySettings    repository.Settings
}

type healthResponse struct {
	Status string `json:"status"`
}

func NewRouter(services ...*app.Service) http.Handler {
	var service *app.Service
	if len(services) > 0 {
		service = services[0]
	}
	return newRouter(service, nil, nil, nil, nil)
}

func NewRouterWithSecrets(service *app.Service, secrets app.SecretWriter, authorizers ...SecretWriteAuthorizer) http.Handler {
	var authorizer SecretWriteAuthorizer
	if len(authorizers) > 0 {
		authorizer = authorizers[0]
	}
	var resolver executioncontext.SecretResolver
	if secretResolver, ok := secrets.(executioncontext.SecretResolver); ok {
		resolver = secretResolver
	}
	return newRouter(service, nil, secrets, resolver, authorizer)
}

func NewRouterWithApplication(services *app.Services, authorizers ...SecretWriteAuthorizer) http.Handler {
	if services == nil {
		return newRouter(nil, nil, nil, nil, nil)
	}
	var authorizer SecretWriteAuthorizer
	if len(authorizers) > 0 {
		authorizer = authorizers[0]
	}
	return newRouterWithProjectAccess(
		services.ControlPlane,
		services.RunEvidence,
		services.Secrets,
		secretResolverFromWriter(services.Secrets),
		authorizer,
		services.Questions,
		app.ReviewServiceFromServices(services),
		services.EventHub,
		services.ProjectAccess,
		services.Auth,
	)
}

func secretResolverFromWriter(writer app.SecretWriter) executioncontext.SecretResolver {
	if resolver, ok := writer.(executioncontext.SecretResolver); ok {
		return resolver
	}
	return nil
}

func newRouter(service *app.Service, runEvidence *app.RunEvidenceService, secretWriter app.SecretWriter, secretResolver executioncontext.SecretResolver, secretWriteAuthorizer SecretWriteAuthorizer, questionServices ...*app.QuestionService) http.Handler {
	var questions *app.QuestionService
	if len(questionServices) > 0 {
		questions = questionServices[0]
	}
	return newRouterWithReviews(service, runEvidence, secretWriter, secretResolver, secretWriteAuthorizer, questions, nil, nil)
}

func newRouterWithReviews(service *app.Service, runEvidence *app.RunEvidenceService, secretWriter app.SecretWriter, secretResolver executioncontext.SecretResolver, secretWriteAuthorizer SecretWriteAuthorizer, questions *app.QuestionService, reviews *app.ReviewService, eventHub *evidence.Hub, authServices ...*app.AuthService) http.Handler {
	var auth *app.AuthService
	if len(authServices) > 0 {
		auth = authServices[0]
	}
	return newRouterWithProjectAccess(service, runEvidence, secretWriter, secretResolver, secretWriteAuthorizer, questions, reviews, eventHub, nil, auth)
}

func newRouterWithProjectAccess(service *app.Service, runEvidence *app.RunEvidenceService, secretWriter app.SecretWriter, secretResolver executioncontext.SecretResolver, secretWriteAuthorizer SecretWriteAuthorizer, questions *app.QuestionService, reviews *app.ReviewService, eventHub *evidence.Hub, projectAccess *app.ProjectAccessService, auth *app.AuthService) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", handleHealth)
	if service == nil {
		return router
	}
	a := &api{
		service:               service,
		auth:                  auth,
		projectAccess:         projectAccess,
		questions:             questions,
		reviews:               reviews,
		runEvidence:           runEvidence,
		eventHub:              eventHub,
		secrets:               secretWriter,
		secretResolver:        secretResolver,
		secretWriteAuthorizer: secretWriteAuthorizer,
		repositorySettings:    repository.SettingsFromEnv(),
	}
	router.Route("/api", func(r chi.Router) {
		if a.auth != nil && a.projectAccess != nil {
			r.Use(a.projectAuthorizationMiddleware)
		}
		if a.auth != nil {
			a.registerAuthRoutes(r)
			a.registerAuthPhase2Routes(r)
			a.registerAuthPhase3GroupRoutes(r)
		}
		if a.projectAccess != nil {
			a.registerProjectAccessRoutes(r)
		}
		a.registerConfigurationRoutes(r)
		if a.service.Runners != nil {
			a.registerRunnerRoutes(r)
		}
		a.registerIssueRunRoutes(r)
		a.registerRunEvidenceRoutes(r)
		if a.questions != nil {
			a.registerQuestionRoutes(r)
		}
		if a.reviews != nil {
			a.registerReviewRoutes(r)
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
		case apiErr.Code == "authentication_failed":
			status = http.StatusUnauthorized
		case apiErr.Code == "forbidden", apiErr.Code == "password_change_required":
			status = http.StatusForbidden
		case apiErr.Code == "bootstrap_closed", apiErr.Code == "conflict", apiErr.Code == "issue_done", apiErr.Code == "agent_unavailable", apiErr.Code == "review_apply_failed", apiErr.Code == "review_evidence_invalid", apiErr.Code == "issue_relationship_exists", apiErr.Code == "last_project_admin":
			status = http.StatusConflict
		case apiErr.Code == "execution_configuration_invalid", apiErr.Code == "provider_model_discovery_unconfigured":
			status = http.StatusUnprocessableEntity
		case apiErr.Code == "provider_credential_unavailable":
			status = http.StatusServiceUnavailable
		case apiErr.Code == "provider_model_discovery_failed":
			status = http.StatusBadGateway
		}
		writeError(w, status, apiErr.Code, apiErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
