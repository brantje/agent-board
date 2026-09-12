package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type projectActorContextKey struct{}

type projectRoleResponse struct {
	Role string `json:"role"`
}

type projectAccessRoleRequest struct {
	Role string `json:"role"`
}

type projectUserAccessResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	Role        string `json:"role"`
}

type projectGroupAccessResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type projectDirectoryUserResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

type projectDirectoryGroupResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (a *api) registerProjectAccessRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/access/effective-role", a.getEffectiveProjectRole)
	r.Get("/projects/{projectID}/access/users", a.listProjectUserAccess)
	r.Put("/projects/{projectID}/access/users/{userID}", a.upsertProjectUserAccess)
	r.Delete("/projects/{projectID}/access/users/{userID}", a.deleteProjectUserAccess)
	r.Get("/projects/{projectID}/access/groups", a.listProjectGroupAccess)
	r.Put("/projects/{projectID}/access/groups/{groupID}", a.upsertProjectGroupAccess)
	r.Delete("/projects/{projectID}/access/groups/{groupID}", a.deleteProjectGroupAccess)
	r.Get("/projects/{projectID}/access/directory/users", a.searchProjectAccessUsers)
	r.Get("/projects/{projectID}/access/directory/groups", a.searchProjectAccessGroups)
}

// projectAuthorizationMiddleware authenticates Project-scoped requests and
// provides defense-in-depth route coverage. The fixed viewer/member/admin
// policy is owned by ProjectAccessService, not by this transport adapter.
func (a *api) projectAuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.auth == nil {
			next.ServeHTTP(w, r)
			return
		}

		collection := isProjectCollectionPath(r.URL.Path)
		projectID, tail, scoped := projectScopeFromPath(r.URL.Path)
		if !collection && !scoped {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", "private, no-store")
		if a.projectAccess == nil {
			writeError(w, http.StatusServiceUnavailable, "project_authorization_unavailable", "project authorization is unavailable")
			return
		}

		actor, ok := a.authActor(w, r)
		if !ok {
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), projectActorContextKey{}, actor))

		if collection {
			switch r.Method {
			case http.MethodGet, http.MethodHead:
				a.listAuthorizedProjects(w, r, actor)
				return
			case http.MethodPost:
				a.createAuthorizedProject(w, r, actor)
				return
			default:
				next.ServeHTTP(w, r)
				return
			}
		}

		if err := a.authorizeProjectRequest(r, actor, projectID, tail); err != nil {
			writeProjectAccessError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) authorizeProjectRequest(r *http.Request, actor app.AuthenticatedUser, projectID string, tail []string) error {
	if len(tail) == 0 {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			return a.projectAccess.AuthorizeRead(r.Context(), actor, projectID)
		}
		return a.projectAccess.AuthorizeAdministration(r.Context(), actor, projectID)
	}

	if tail[0] == "access" {
		if len(tail) >= 2 && tail[1] == "effective-role" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			return a.projectAccess.AuthorizeRead(r.Context(), actor, projectID)
		}
		return a.projectAccess.AuthorizeAdministration(r.Context(), actor, projectID)
	}

	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return a.projectAccess.AuthorizeRead(r.Context(), actor, projectID)
	}

	switch tail[0] {
	case "providers", "model-profiles", "runtimes", "agents", "runners", "secrets":
		return a.projectAccess.AuthorizeAdministration(r.Context(), actor, projectID)
	default:
		return a.projectAccess.AuthorizeWorkflowMutation(r.Context(), actor, projectID)
	}
}

func isProjectCollectionPath(path string) bool {
	trimmed := strings.TrimSuffix(path, "/")
	return trimmed == "/api/projects"
}

func projectScopeFromPath(path string) (string, []string, bool) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "projects" || !validUUID(parts[2]) {
		return "", nil, false
	}
	return parts[2], parts[3:], true
}

func writeProjectAccessError(w http.ResponseWriter, err error) {
	if apiErr, ok := app.AsError(err); ok && (apiErr.Code == "forbidden" || apiErr.Code == "password_change_required") {
		writeAuthBoundaryError(w, err)
		return
	}
	writeAppError(w, err)
}

func projectActor(r *http.Request) (app.AuthenticatedUser, bool) {
	actor, ok := r.Context().Value(projectActorContextKey{}).(app.AuthenticatedUser)
	return actor, ok
}

func (a *api) listAuthorizedProjects(w http.ResponseWriter, r *http.Request, actor app.AuthenticatedUser) {
	values, err := a.projectAccess.ListProjects(r.Context(), actor)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]ProjectDTO, 0, len(values))
	for _, value := range values {
		out = append(out, projectDTO(value))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) createAuthorizedProject(w http.ResponseWriter, r *http.Request, actor app.AuthenticatedUser) {
	var req CreateProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	value, err := a.projectAccess.CreateProject(r.Context(), actor, store.Project{
		AllowInternalRunner: req.AllowInternalRunner,
		Name:                req.Name,
		IssuePrefix:         req.IssuePrefix,
		SourceType:          req.SourceType,
		CloneURL:            req.CloneURL,
		SourceRef:           req.SourceRef,
		RepositoryPath:      req.RepositoryPath,
		DefaultBranch:       req.DefaultBranch,
		WorkflowSettings:    req.WorkflowSettings,
	})
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectDTO(value))
}

func (a *api) getEffectiveProjectRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := projectActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return
	}
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	role, err := a.projectAccess.EffectiveRole(r.Context(), actor, projectID)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectRoleResponse{Role: role})
}

func (a *api) listProjectUserAccess(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	values, err := a.projectAccess.ListUserAccess(r.Context(), actor, projectID)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]projectUserAccessResponse, 0, len(values))
	for _, value := range values {
		out = append(out, projectUserAccessResponse{ID: value.User.ID, Username: value.User.Username, Email: value.User.Email, DisplayName: value.User.DisplayName, Status: value.User.Status, Role: value.Access.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) upsertProjectUserAccess(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "userID")
	if !ok {
		return
	}
	var req projectAccessRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	value, err := a.projectAccess.UpsertUserAccess(r.Context(), actor, store.ProjectUserAccess{ProjectID: projectID, UserID: userID, Role: req.Role})
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectRoleResponse{Role: value.Role})
}

func (a *api) deleteProjectUserAccess(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "userID")
	if !ok {
		return
	}
	if err := a.projectAccess.DeleteUserAccess(r.Context(), actor, projectID, userID); err != nil {
		writeProjectAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listProjectGroupAccess(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	values, err := a.projectAccess.ListGroupAccess(r.Context(), actor, projectID)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]projectGroupAccessResponse, 0, len(values))
	for _, value := range values {
		out = append(out, projectGroupAccessResponse{ID: value.Group.ID, Name: value.Group.Name, Role: value.Access.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) upsertProjectGroupAccess(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	groupID, ok := pathUUID(w, r, "groupID")
	if !ok {
		return
	}
	var req projectAccessRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	value, err := a.projectAccess.UpsertGroupAccess(r.Context(), actor, store.ProjectGroupAccess{ProjectID: projectID, GroupID: groupID, Role: req.Role})
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectRoleResponse{Role: value.Role})
}

func (a *api) deleteProjectGroupAccess(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	groupID, ok := pathUUID(w, r, "groupID")
	if !ok {
		return
	}
	if err := a.projectAccess.DeleteGroupAccess(r.Context(), actor, projectID, groupID); err != nil {
		writeProjectAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) searchProjectAccessUsers(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	values, err := a.projectAccess.SearchUsers(r.Context(), actor, projectID, r.URL.Query().Get("q"))
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]projectDirectoryUserResponse, 0, len(values))
	for _, value := range values {
		out = append(out, projectDirectoryUserResponse{ID: value.ID, Username: value.Username, Email: value.Email, DisplayName: value.DisplayName})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) searchProjectAccessGroups(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := a.projectAccessActorAndProject(w, r)
	if !ok {
		return
	}
	values, err := a.projectAccess.SearchGroups(r.Context(), actor, projectID, r.URL.Query().Get("q"))
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]projectDirectoryGroupResponse, 0, len(values))
	for _, value := range values {
		out = append(out, projectDirectoryGroupResponse{ID: value.ID, Name: value.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) projectAccessActorAndProject(w http.ResponseWriter, r *http.Request) (app.AuthenticatedUser, string, bool) {
	actor, ok := projectActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return app.AuthenticatedUser{}, "", false
	}
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return app.AuthenticatedUser{}, "", false
	}
	return actor, projectID, true
}
