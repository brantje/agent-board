package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectAccessHTTPStore struct {
	projects        []store.Project
	visibleByUser   map[string][]store.Project
	roles           map[string]string
	users           map[string]store.User
	groups          map[string]store.Group
	userGrants      map[string]store.ProjectUserAccess
	groupGrants     map[string]store.ProjectGroupAccess
	createdBy       string
	deleteUserError error
}

func newProjectAccessHTTPStore() *projectAccessHTTPStore {
	project := store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB", SourceType: store.ProjectSourceLocal, RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}
	return &projectAccessHTTPStore{
		projects:      []store.Project{project},
		visibleByUser: map[string][]store.Project{},
		roles:         map[string]string{},
		users:         map[string]store.User{},
		groups:        map[string]store.Group{},
		userGrants:    map[string]store.ProjectUserAccess{},
		groupGrants:   map[string]store.ProjectGroupAccess{},
	}
}

func projectGrantKey(projectID, subjectID string) string { return projectID + ":" + subjectID }

func (s *projectAccessHTTPStore) CreateProjectWithAdmin(_ context.Context, input store.Project, userID string) (store.Project, error) {
	input.ID = otherID
	if input.SourceType == "" {
		input.SourceType = store.ProjectSourceLocal
	}
	if input.DefaultBranch == "" {
		input.DefaultBranch = "main"
	}
	if len(input.WorkflowSettings) == 0 {
		input.WorkflowSettings = store.EmptyObject
	}
	s.createdBy = userID
	s.projects = append(s.projects, input)
	s.userGrants[projectGrantKey(input.ID, userID)] = store.ProjectUserAccess{ProjectID: input.ID, UserID: userID, Role: store.ProjectRoleAdmin}
	return input, nil
}

func (s *projectAccessHTTPStore) ListProjectsForUser(_ context.Context, userID string) ([]store.Project, error) {
	return append([]store.Project(nil), s.visibleByUser[userID]...), nil
}

func (s *projectAccessHTTPStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	role, ok := s.roles[projectGrantKey(projectID, userID)]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func (s *projectAccessHTTPStore) ListProjectUserAccess(_ context.Context, projectID string) ([]store.ProjectUserAccessView, error) {
	result := make([]store.ProjectUserAccessView, 0)
	for _, grant := range s.userGrants {
		if grant.ProjectID == projectID {
			result = append(result, store.ProjectUserAccessView{Access: grant, User: s.users[grant.UserID]})
		}
	}
	return result, nil
}

func (s *projectAccessHTTPStore) UpsertProjectUserAccess(_ context.Context, input store.ProjectUserAccess) (store.ProjectUserAccess, error) {
	s.userGrants[projectGrantKey(input.ProjectID, input.UserID)] = input
	s.roles[projectGrantKey(input.ProjectID, input.UserID)] = input.Role
	return input, nil
}

func (s *projectAccessHTTPStore) DeleteProjectUserAccess(_ context.Context, projectID, userID string) error {
	if s.deleteUserError != nil {
		return s.deleteUserError
	}
	key := projectGrantKey(projectID, userID)
	if _, ok := s.userGrants[key]; !ok {
		return store.ErrNotFound
	}
	delete(s.userGrants, key)
	delete(s.roles, key)
	return nil
}

func (s *projectAccessHTTPStore) ListProjectGroupAccess(_ context.Context, projectID string) ([]store.ProjectGroupAccessView, error) {
	result := make([]store.ProjectGroupAccessView, 0)
	for _, grant := range s.groupGrants {
		if grant.ProjectID == projectID {
			result = append(result, store.ProjectGroupAccessView{Access: grant, Group: s.groups[grant.GroupID]})
		}
	}
	return result, nil
}

func (s *projectAccessHTTPStore) UpsertProjectGroupAccess(_ context.Context, input store.ProjectGroupAccess) (store.ProjectGroupAccess, error) {
	s.groupGrants[projectGrantKey(input.ProjectID, input.GroupID)] = input
	return input, nil
}

func (s *projectAccessHTTPStore) DeleteProjectGroupAccess(_ context.Context, projectID, groupID string) error {
	key := projectGrantKey(projectID, groupID)
	if _, ok := s.groupGrants[key]; !ok {
		return store.ErrNotFound
	}
	delete(s.groupGrants, key)
	return nil
}

func (s *projectAccessHTTPStore) GetProjectAccessUser(_ context.Context, userID string) (store.User, error) {
	user, ok := s.users[userID]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return user, nil
}

func (s *projectAccessHTTPStore) ListProjectAccessUsers(context.Context) ([]store.User, error) {
	result := make([]store.User, 0, len(s.users))
	for _, user := range s.users {
		result = append(result, user)
	}
	return result, nil
}

func (s *projectAccessHTTPStore) ListProjectAccessGroups(context.Context) ([]store.Group, error) {
	result := make([]store.Group, 0, len(s.groups))
	for _, group := range s.groups {
		result = append(result, group)
	}
	return result, nil
}

type projectAccessHTTPFixture struct {
	handler http.Handler
	auth    *app.AuthService
	authDB  *authHTTPStore
	access  *projectAccessHTTPStore
}

func newProjectAccessHTTPFixture(t *testing.T) *projectAccessHTTPFixture {
	t.Helper()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	authDB := newAuthHTTPStore()
	authService, err := app.NewAuthService(authDB, app.AuthServiceConfig{
		Now:        func() time.Time { return now },
		Random:     &authHTTPRandom{},
		SigningKey: []byte("01234567890123456789012345678901"),
	})
	if err != nil {
		t.Fatal(err)
	}
	controlPlane := app.New(&fakeControlPlaneStore{})
	accessDB := newProjectAccessHTTPStore()
	accessService, err := app.NewProjectAccessService(controlPlane, accessDB)
	if err != nil {
		t.Fatal(err)
	}
	services := &app.Services{ControlPlane: controlPlane, Auth: authService, ProjectAccess: accessService}
	return &projectAccessHTTPFixture{handler: NewRouterWithApplication(services), auth: authService, authDB: authDB, access: accessDB}
}

func (f *projectAccessHTTPFixture) createUser(t *testing.T, username, deploymentRole string) (store.User, string) {
	t.Helper()
	password := "long-enough-password"
	user, err := f.authDB.CreateUser(t.Context(), store.User{
		Username: username, Email: username + "@example.com", DisplayName: username,
		DeploymentRole: deploymentRole, Status: store.UserStatusActive, AuthVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err = f.auth.SetPassword(t.Context(), user.ID, password, false)
	if err != nil {
		t.Fatal(err)
	}
	f.access.users[user.ID] = user
	tokens, err := f.auth.Login(t.Context(), username, password)
	if err != nil {
		t.Fatal(err)
	}
	return user, tokens.AccessToken
}

func bearer(token string) map[string]string { return map[string]string{"Authorization": "Bearer " + token} }

func TestProjectAccessHTTPFiltersDiscoveryAndPreservesNotFoundIsolation(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	viewer, token := fixture.createUser(t, "viewer", store.DeploymentRoleMember)
	fixture.access.visibleByUser[viewer.ID] = fixture.access.projects[:1]
	fixture.access.roles[projectGrantKey(projectID, viewer.ID)] = store.ProjectRoleViewer

	list := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects", "", bearer(token))
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var projects []ProjectDTO
	if err := json.Unmarshal(list.Body.Bytes(), &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != projectID {
		t.Fatalf("visible projects = %+v", projects)
	}

	read := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID+"/issues", "", bearer(token))
	if read.Code != http.StatusOK {
		t.Fatalf("viewer read status=%d body=%s", read.Code, read.Body.String())
	}
	write := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects/"+projectID+"/issues", `{"title":"blocked"}`, bearer(token))
	if write.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation status=%d body=%s", write.Code, write.Body.String())
	}

	_, unrelatedToken := fixture.createUser(t, "unrelated", store.DeploymentRoleMember)
	hidden := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID, "", bearer(unrelatedToken))
	if hidden.Code != http.StatusNotFound || !stringsContainsJSONCode(hidden.Body.String(), "project_not_found") {
		t.Fatalf("unrelated project status=%d body=%s", hidden.Code, hidden.Body.String())
	}
}

func TestProjectAccessHTTPMemberAndAdminRoleBoundaries(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	member, memberToken := fixture.createUser(t, "member", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, member.ID)] = store.ProjectRoleMember

	created := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects/"+projectID+"/issues", `{"title":"allowed"}`, bearer(memberToken))
	if created.Code != http.StatusCreated {
		t.Fatalf("member issue create status=%d body=%s", created.Code, created.Body.String())
	}
	settings := authHTTPRequest(t, fixture.handler, http.MethodPatch, "/api/projects/"+projectID, `{"name":"Nope"}`, bearer(memberToken))
	if settings.Code != http.StatusForbidden {
		t.Fatalf("member project settings status=%d body=%s", settings.Code, settings.Body.String())
	}

	admin, adminToken := fixture.createUser(t, "project-admin", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, admin.ID)] = store.ProjectRoleAdmin
	fixture.access.userGrants[projectGrantKey(projectID, admin.ID)] = store.ProjectUserAccess{ProjectID: projectID, UserID: admin.ID, Role: store.ProjectRoleAdmin}
	target, _ := fixture.createUser(t, "target", store.DeploymentRoleMember)
	grant := authHTTPRequest(t, fixture.handler, http.MethodPut, fmt.Sprintf("/api/projects/%s/access/users/%s", projectID, target.ID), `{"role":"viewer"}`, bearer(adminToken))
	if grant.Code != http.StatusOK || fixture.access.userGrants[projectGrantKey(projectID, target.ID)].Role != store.ProjectRoleViewer {
		t.Fatalf("admin grant status=%d body=%s grant=%+v", grant.Code, grant.Body.String(), fixture.access.userGrants[projectGrantKey(projectID, target.ID)])
	}
}

func TestProjectAccessHTTPDeploymentAdminIsImplicitProjectAdmin(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	_, token := fixture.createUser(t, "deployment-admin", store.DeploymentRoleAdmin)

	role := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID+"/access/effective-role", "", bearer(token))
	if role.Code != http.StatusOK || !stringsContainsJSONCode(role.Body.String(), store.ProjectRoleAdmin) {
		t.Fatalf("deployment admin role status=%d body=%s", role.Code, role.Body.String())
	}
	list := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects", "", bearer(token))
	if list.Code != http.StatusOK || !stringsContainsJSONCode(list.Body.String(), projectID) {
		t.Fatalf("deployment admin list status=%d body=%s", list.Code, list.Body.String())
	}
}

func TestProjectAccessHTTPProjectCreationGrantsCreatorDirectAdmin(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	creator, token := fixture.createUser(t, "creator", store.DeploymentRoleMember)

	response := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects", `{"name":"Created","issuePrefix":"NEW","sourceType":"local","repositoryPath":"/repo/new","defaultBranch":"main"}`, bearer(token))
	if response.Code != http.StatusCreated {
		t.Fatalf("create project status=%d body=%s", response.Code, response.Body.String())
	}
	if fixture.access.createdBy != creator.ID {
		t.Fatalf("creator admin user = %q, want %q", fixture.access.createdBy, creator.ID)
	}
	grant := fixture.access.userGrants[projectGrantKey(otherID, creator.ID)]
	if grant.Role != store.ProjectRoleAdmin {
		t.Fatalf("creator grant = %+v", grant)
	}
}

func TestProjectAccessHTTPLastDirectAdminConflictIsExplicit(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	admin, token := fixture.createUser(t, "sole-admin", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, admin.ID)] = store.ProjectRoleAdmin
	fixture.access.userGrants[projectGrantKey(projectID, admin.ID)] = store.ProjectUserAccess{ProjectID: projectID, UserID: admin.ID, Role: store.ProjectRoleAdmin}
	fixture.access.deleteUserError = store.ErrLastProjectAdmin

	response := authHTTPRequest(t, fixture.handler, http.MethodDelete, fmt.Sprintf("/api/projects/%s/access/users/%s", projectID, admin.ID), "", bearer(token))
	if response.Code != http.StatusConflict || !stringsContainsJSONCode(response.Body.String(), "last_project_admin") {
		t.Fatalf("last admin delete status=%d body=%s", response.Code, response.Body.String())
	}
	if !errors.Is(fixture.access.deleteUserError, store.ErrLastProjectAdmin) {
		t.Fatal("test fixture lost last-admin error")
	}
}

func stringsContainsJSONCode(body, value string) bool {
	return strings.Contains(body, `"`+value+`"`) || strings.Contains(body, value)
}
