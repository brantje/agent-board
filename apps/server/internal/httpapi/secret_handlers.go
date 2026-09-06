package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/go-chi/chi/v5"
)

type secretUpsertRequest struct {
	Ref   string `json:"ref"`
	Value string `json:"value"`
}

type secretMetadataResponse struct {
	Ref       string  `json:"ref"`
	ProjectID *string `json:"projectId"`
}

func (a *api) registerSecretRoutes(r chi.Router) {
	r.Put("/secrets", a.putGlobalSecret)
	r.Put("/projects/{projectID}/secrets", a.putProjectSecret)
}

func (a *api) putGlobalSecret(w http.ResponseWriter, r *http.Request) {
	a.putSecret(w, r, nil)
}

func (a *api) putProjectSecret(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	if _, err := a.service.GetProject(r.Context(), projectID); err != nil {
		writeAppError(w, err)
		return
	}
	a.putSecret(w, r, &projectID)
}

func (a *api) putSecret(w http.ResponseWriter, r *http.Request, projectID *string) {
	var input secretUpsertRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Ref = strings.TrimSpace(input.Ref)
	if input.Ref == "" || input.Value == "" {
		writeError(w, http.StatusBadRequest, "invalid_secret", "ref and value are required")
		return
	}
	metadata, err := a.secrets.Put(r.Context(), secrets.Scope{ProjectID: projectID}, input.Ref, []byte(input.Value))
	if err != nil {
		if errors.Is(err, secrets.ErrInvalidSecret) {
			writeError(w, http.StatusBadRequest, "invalid_secret", "ref and value are required")
			return
		}
		writeError(w, http.StatusInternalServerError, "secret_write_failed", "secret could not be stored")
		return
	}
	writeJSON(w, http.StatusOK, secretMetadataResponse{Ref: metadata.Ref, ProjectID: metadata.ProjectID})
}
