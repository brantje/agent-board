package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectAccessHTTPRejectsMalformedProjectAndGrantBodies(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	_, memberToken := fixture.createUser(t, "malformed-project-member", store.DeploymentRoleMember)

	malformedProject := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects", `{`, bearer(memberToken))
	if malformedProject.Code != http.StatusBadRequest || !stringsContainsJSONCode(malformedProject.Body.String(), "invalid_request") {
		t.Fatalf("malformed project status=%d body=%s", malformedProject.Code, malformedProject.Body.String())
	}

	invalidProject := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects", `{}`, bearer(memberToken))
	if invalidProject.Code != http.StatusBadRequest || !stringsContainsJSONCode(invalidProject.Body.String(), "invalid_argument") {
		t.Fatalf("invalid project status=%d body=%s", invalidProject.Code, invalidProject.Body.String())
	}

	adminFixture, _, adminToken := projectAdminFixture(t)
	malformedUserGrant := authHTTPRequest(t, adminFixture.handler, http.MethodPut,
		"/api/projects/"+projectID+"/access/users/"+projectAccessUnknownUserID, `{`, bearer(adminToken))
	if malformedUserGrant.Code != http.StatusBadRequest || !stringsContainsJSONCode(malformedUserGrant.Body.String(), "invalid_request") {
		t.Fatalf("malformed user grant status=%d body=%s", malformedUserGrant.Code, malformedUserGrant.Body.String())
	}

	malformedGroupGrant := authHTTPRequest(t, adminFixture.handler, http.MethodPut,
		"/api/projects/"+projectID+"/access/groups/"+projectAccessUnknownGroupID, `{`, bearer(adminToken))
	if malformedGroupGrant.Code != http.StatusBadRequest || !stringsContainsJSONCode(malformedGroupGrant.Body.String(), "invalid_request") {
		t.Fatalf("malformed group grant status=%d body=%s", malformedGroupGrant.Code, malformedGroupGrant.Body.String())
	}
}

func TestProjectAccessHTTPMissingGrantDeletesReturnNotFound(t *testing.T) {
	fixture, _, token := projectAdminFixture(t)

	missingUser := authHTTPRequest(t, fixture.handler, http.MethodDelete,
		"/api/projects/"+projectID+"/access/users/"+projectAccessUnknownUserID, "", bearer(token))
	if missingUser.Code != http.StatusNotFound || !stringsContainsJSONCode(missingUser.Body.String(), "project_access_not_found") {
		t.Fatalf("missing user grant status=%d body=%s", missingUser.Code, missingUser.Body.String())
	}

	missingGroup := authHTTPRequest(t, fixture.handler, http.MethodDelete,
		"/api/projects/"+projectID+"/access/groups/"+projectAccessUnknownGroupID, "", bearer(token))
	if missingGroup.Code != http.StatusNotFound || !stringsContainsJSONCode(missingGroup.Body.String(), "project_access_not_found") {
		t.Fatalf("missing group grant status=%d body=%s", missingGroup.Code, missingGroup.Body.String())
	}
}

func TestProjectAuthorizationMiddlewareLegacyAndUnsupportedCollectionPaths(t *testing.T) {
	legacy := &api{}
	called := false
	handler := legacy.projectAuthorizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/issues", nil))
	if !called || recorder.Code != http.StatusNoContent {
		t.Fatalf("legacy middleware called=%v status=%d", called, recorder.Code)
	}

	fixture := newProjectAccessHTTPFixture(t)
	_, token := fixture.createUser(t, "unsupported-collection-method", store.DeploymentRoleMember)
	response := authHTTPRequest(t, fixture.handler, http.MethodPut, "/api/projects", `{}`, bearer(token))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported collection method status=%d body=%s", response.Code, response.Body.String())
	}
}
