package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectAccessHandlersRejectMissingTrustedActor(t *testing.T) {
	a := &api{}
	handlers := map[string]http.HandlerFunc{
		"effective role":    a.getEffectiveProjectRole,
		"list user access":  a.listProjectUserAccess,
		"upsert user access": a.upsertProjectUserAccess,
		"delete user access": a.deleteProjectUserAccess,
		"list group access": a.listProjectGroupAccess,
		"upsert group access": a.upsertProjectGroupAccess,
		"delete group access": a.deleteProjectGroupAccess,
		"search users":      a.searchProjectAccessUsers,
		"search groups":     a.searchProjectAccessGroups,
	}

	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/projects/11111111-1111-4111-8111-111111111111/access", nil)
			res := httptest.NewRecorder()

			handler(res, req)

			if res.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
			if !stringsContainsJSONCode(res.Body.String(), "authentication_failed") {
				t.Fatalf("body = %s", res.Body.String())
			}
		})
	}
}

func TestProjectAccessActorHelperRejectsMissingActor(t *testing.T) {
	a := &api{}
	req := httptest.NewRequest(http.MethodGet, "/api/projects/11111111-1111-4111-8111-111111111111/access/users", nil)
	res := httptest.NewRecorder()

	actor, projectID, ok := a.projectAccessActorAndProject(res, req)
	if ok || actor.ID != "" || projectID != "" {
		t.Fatalf("actor=%+v projectID=%q ok=%v", actor, projectID, ok)
	}
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}
