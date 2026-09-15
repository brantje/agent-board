package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type placementHTTPStore struct {
	fakeControlPlaneStore
	calls int
	input store.IssuePlacement
	actor json.RawMessage
}

func (s *placementHTTPStore) PlaceIssue(_ context.Context, input store.IssuePlacement, actor json.RawMessage) (store.IssueMutationResult, error) {
	s.calls++
	s.input = input
	s.actor = append(json.RawMessage(nil), actor...)
	issue := issueFixture("TODO")
	if input.Status != nil {
		issue.Status = *input.Status
	}
	return store.IssueMutationResult{Issue: issue}, nil
}

func newPlacementAccessHTTPFixture(t *testing.T) (*projectAccessHTTPFixture, *placementHTTPStore) {
	t.Helper()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	authDB := newAuthHTTPStore()
	authService, err := app.NewAuthService(authDB, app.AuthServiceConfig{
		Now:        func() time.Time { return now },
		Random:     &authHTTPRandom{},
		SigningKey: []byte("01234567890123456789012345678901"),
	})
	if err != nil {
		t.Fatal(err)
	}
	placementStore := &placementHTTPStore{}
	controlPlane := app.New(placementStore)
	accessDB := newProjectAccessHTTPStore()
	accessService, err := app.NewProjectAccessService(controlPlane, accessDB)
	if err != nil {
		t.Fatal(err)
	}
	services := &app.Services{ControlPlane: controlPlane, Auth: authService, ProjectAccess: accessService}
	fixture := &projectAccessHTTPFixture{handler: NewRouterWithApplication(services), auth: authService, authDB: authDB, access: accessDB}
	return fixture, placementStore
}

func TestIssuePlacementHTTPUsesIssueKeysAndProjectMutationAuthorization(t *testing.T) {
	fixture, placementStore := newPlacementAccessHTTPFixture(t)
	member, memberToken := fixture.createUser(t, "placement-member", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, member.ID)] = store.ProjectRoleMember

	response := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueKey+"/placement", `{"status":"REVIEW","beforeId":null,"afterId":"`+otherIssueKey+`"}`, bearer(memberToken))
	if response.Code != http.StatusOK {
		t.Fatalf("member placement status=%d body=%s", response.Code, response.Body.String())
	}
	if placementStore.calls != 1 {
		t.Fatalf("placement calls=%d, want 1", placementStore.calls)
	}
	if placementStore.input.IssueID != issueID || placementStore.input.AfterID == nil || *placementStore.input.AfterID != otherID || placementStore.input.BeforeID != nil {
		t.Fatalf("resolved placement input = %+v", placementStore.input)
	}
	if placementStore.input.Status == nil || *placementStore.input.Status != "REVIEW" {
		t.Fatalf("placement status = %+v, want REVIEW", placementStore.input.Status)
	}

	viewer, viewerToken := fixture.createUser(t, "placement-viewer", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, viewer.ID)] = store.ProjectRoleViewer
	response = authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueKey+"/placement", `{"beforeId":null,"afterId":null}`, bearer(viewerToken))
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer placement status=%d body=%s", response.Code, response.Body.String())
	}
	if placementStore.calls != 1 {
		t.Fatalf("viewer reached placement store; calls=%d", placementStore.calls)
	}

	_, adminToken := fixture.createUser(t, "placement-admin", store.DeploymentRoleAdmin)
	response = authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueKey+"/placement", `{"beforeId":null,"afterId":null}`, bearer(adminToken))
	if response.Code != http.StatusOK {
		t.Fatalf("admin placement status=%d body=%s", response.Code, response.Body.String())
	}
	if placementStore.calls != 2 {
		t.Fatalf("admin placement calls=%d, want 2", placementStore.calls)
	}
}

func TestIssuePlacementHTTPRequiresExplicitNullableAnchors(t *testing.T) {
	storeImpl := &placementHTTPStore{}
	router := NewRouter(app.New(storeImpl))
	response := authHTTPRequest(t, router, http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueKey+"/placement", `{"status":"TODO","beforeId":null}`, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing anchor status=%d body=%s", response.Code, response.Body.String())
	}
	if storeImpl.calls != 0 {
		t.Fatalf("invalid request reached placement store; calls=%d", storeImpl.calls)
	}
}
