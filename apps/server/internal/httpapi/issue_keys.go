package httpapi

import (
	"context"
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func pathIssueKey(w http.ResponseWriter, r *http.Request, projectID string, resolve func(context.Context, string, string) (string, error)) (string, bool) {
	ref := chi.URLParam(r, "issueID")
	if validUUID(ref) {
		writeError(w, http.StatusBadRequest, "invalid_id", "issueID must be an issue key")
		return "", false
	}
	if !store.ValidIssueKey(ref) {
		writeError(w, http.StatusBadRequest, "invalid_id", "issueID must be an issue key")
		return "", false
	}
	issueUUID, err := resolve(r.Context(), projectID, ref)
	if err != nil {
		writeAppError(w, err)
		return "", false
	}
	return issueUUID, true
}

func queryIssueKey(w http.ResponseWriter, r *http.Request, name string, projectID string, resolve func(context.Context, string, string) (string, error)) (*string, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return nil, true
	}
	if validUUID(value) {
		writeError(w, http.StatusBadRequest, "invalid_id", name+" must be an issue key")
		return nil, false
	}
	if !store.ValidIssueKey(value) {
		writeError(w, http.StatusBadRequest, "invalid_id", name+" must be an issue key")
		return nil, false
	}
	issueUUID, err := resolve(r.Context(), projectID, value)
	if err != nil {
		writeAppError(w, err)
		return nil, false
	}
	return &issueUUID, true
}

func bodyIssueKey(w http.ResponseWriter, r *http.Request, projectID, value, field string, resolve func(context.Context, string, string) (string, error)) (string, bool) {
	if validUUID(value) {
		writeError(w, http.StatusBadRequest, "invalid_id", field+" must be an issue key")
		return "", false
	}
	if !store.ValidIssueKey(value) {
		writeError(w, http.StatusBadRequest, "invalid_id", field+" must be an issue key")
		return "", false
	}
	issueUUID, err := resolve(r.Context(), projectID, value)
	if err != nil {
		writeAppError(w, err)
		return "", false
	}
	return issueUUID, true
}

func (a *api) issueKeyMap(ctx context.Context, projectID string) (map[string]string, error) {
	issues, err := a.service.ListIssues(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(issues))
	for _, issue := range issues {
		out[issue.ID] = issue.Key
	}
	return out, nil
}

func issueKeyForUUID(keys map[string]string, issueUUID string) string {
	if key, ok := keys[issueUUID]; ok {
		return key
	}
	return issueUUID
}

func issueKeysFromResolved(sourceUUID, sourceKey, targetUUID, targetKey string) map[string]string {
	return map[string]string{sourceUUID: sourceKey, targetUUID: targetKey}
}

func issueKeysFromPath(issueUUID, issueKey string) map[string]string {
	return map[string]string{issueUUID: issueKey}
}
