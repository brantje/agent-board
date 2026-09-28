package httpapi

import (
	"net/http"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (a *api) listSourceConnections(w http.ResponseWriter, r *http.Request, scope *string) {
	values, err := a.service.ListSourceConnections(r.Context(), scope)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]SourceConnectionDTO, 0, len(values))
	for _, value := range values {
		out = append(out, sourceConnectionDTO(value))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) createSourceConnection(w http.ResponseWriter, r *http.Request, scope *string) {
	var req SourceConnectionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	plain := ""
	if req.Credential != nil {
		plain = strings.TrimSpace(*req.Credential)
		if plain == "" {
			writeError(w, http.StatusBadRequest, "invalid_argument", "credential must not be blank")
			return
		}
		if a.secrets == nil {
			writeError(w, http.StatusServiceUnavailable, "secret_storage_unavailable", "Secret storage is not configured for this deployment.")
			return
		}
	}
	value, err := a.service.CreateSourceConnection(r.Context(), store.SourceConnection{
		ProjectID: scope, Kind: req.Kind, Name: req.Name, BaseURL: req.BaseURL,
		ExternalAccountID: req.ExternalAccountID, Enabled: boolDefault(req.Enabled, true),
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	if plain != "" {
		ref := "source-connection:" + value.ID
		metadata, putErr := a.secrets.Put(r.Context(), secrets.Scope{ProjectID: scope}, ref, []byte(plain))
		if putErr != nil {
			writeAppError(w, putErr)
			return
		}
		value.CredentialRef = &metadata.Ref
		value, err = a.service.UpdateSourceConnection(r.Context(), scope, value)
		if err != nil {
			writeAppError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, sourceConnectionDTO(value))
}

func (a *api) getSourceConnection(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	value, err := a.service.GetSourceConnection(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sourceConnectionDTO(value))
}

func (a *api) updateSourceConnection(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var req SourceConnectionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	current, err := a.service.GetSourceConnectionForMutation(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	plain := ""
	if req.Credential != nil {
		plain = strings.TrimSpace(*req.Credential)
		if plain == "" {
			writeError(w, http.StatusBadRequest, "invalid_argument", "credential must not be blank")
			return
		}
		if a.secrets == nil {
			writeError(w, http.StatusServiceUnavailable, "secret_storage_unavailable", "Secret storage is not configured for this deployment.")
			return
		}
	}
	current.Name = req.Name
	current.Kind = req.Kind
	current.BaseURL = req.BaseURL
	current.ExternalAccountID = req.ExternalAccountID
	current.Enabled = boolDefault(req.Enabled, current.Enabled)
	if plain != "" {
		ref := "source-connection:" + current.ID
		if current.CredentialRef != nil && strings.TrimSpace(*current.CredentialRef) != "" {
			ref = strings.TrimSpace(*current.CredentialRef)
		}
		metadata, putErr := a.secrets.Put(r.Context(), secrets.Scope{ProjectID: current.ProjectID}, ref, []byte(plain))
		if putErr != nil {
			writeAppError(w, putErr)
			return
		}
		current.CredentialRef = &metadata.Ref
	}
	value, err := a.service.UpdateSourceConnection(r.Context(), scope, current)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sourceConnectionDTO(value))
}

func (a *api) listSourceRepositories(w http.ResponseWriter, r *http.Request, scope *string) {
	connectionID, ok := resourceID(w, r)
	if !ok {
		return
	}
	values, err := a.service.ListSourceRepositories(r.Context(), scope, connectionID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]SourceRepositoryDTO, 0, len(values))
	for _, value := range values {
		out = append(out, sourceRepositoryDTO(value))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) getSourceRepository(w http.ResponseWriter, r *http.Request, scope *string) {
	connectionID, ok := resourceID(w, r)
	if !ok {
		return
	}
	repositoryID, ok := pathUUID(w, r, "repositoryID")
	if !ok {
		return
	}
	value, err := a.service.GetSourceRepository(r.Context(), scope, connectionID, repositoryID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sourceRepositoryDTO(value))
}

func (a *api) listGlobalSourceConnections(w http.ResponseWriter, r *http.Request) { a.listSourceConnections(w, r, nil) }
func (a *api) createGlobalSourceConnection(w http.ResponseWriter, r *http.Request) { a.createSourceConnection(w, r, nil) }
func (a *api) getGlobalSourceConnection(w http.ResponseWriter, r *http.Request) { a.getSourceConnection(w, r, nil) }
func (a *api) updateGlobalSourceConnection(w http.ResponseWriter, r *http.Request) { a.updateSourceConnection(w, r, nil) }
func (a *api) listGlobalSourceRepositories(w http.ResponseWriter, r *http.Request) { a.listSourceRepositories(w, r, nil) }
func (a *api) getGlobalSourceRepository(w http.ResponseWriter, r *http.Request) { a.getSourceRepository(w, r, nil) }

func (a *api) listProjectSourceConnections(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFromProject(w, r)
	if ok { a.listSourceConnections(w, r, scope) }
}
func (a *api) createProjectSourceConnection(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFromProject(w, r)
	if ok { a.createSourceConnection(w, r, scope) }
}
func (a *api) getProjectSourceConnection(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFromProject(w, r)
	if ok { a.getSourceConnection(w, r, scope) }
}
func (a *api) updateProjectSourceConnection(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFromProject(w, r)
	if ok { a.updateSourceConnection(w, r, scope) }
}
func (a *api) listProjectSourceRepositories(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFromProject(w, r)
	if ok { a.listSourceRepositories(w, r, scope) }
}
func (a *api) getProjectSourceRepository(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFromProject(w, r)
	if ok { a.getSourceRepository(w, r, scope) }
}
