package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/go-chi/chi/v5"
)

type api struct{ service *app.Service }

type healthResponse struct {
	Status string `json:"status"`
}

func NewRouter(services ...*app.Service) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", handleHealth)
	if len(services) == 0 || services[0] == nil {
		return router
	}
	a := &api{service: services[0]}
	router.Route("/api", func(r chi.Router) {
		a.registerConfigurationRoutes(r)
		a.registerIssueRunRoutes(r)
	})
	return router
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
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
		case apiErr.Code == "conflict":
			status = http.StatusConflict
		}
		writeError(w, status, apiErr.Code, apiErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
