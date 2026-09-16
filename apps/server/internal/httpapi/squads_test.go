package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	squadID            = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	squadMemberAgentID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

type squadHTTPStore struct {
	*fakeControlPlaneStore
	squads map[string]store.Squad
	agents map[string]store.Agent
}

func newSquadHTTPStore() *squadHTTPStore {
	project := projectID
	return &squadHTTPStore{
		fakeControlPlaneStore: &fakeControlPlaneStore{},
		squads:                make(map[string]store.Squad),
		agents: map[string]store.Agent{
			agentID:             {ID: agentID, ProjectID: &project, State: "ENABLED"},
			squadMemberAgentID: {ID: squadMemberAgentID, ProjectID: &project, State: "ENABLED"},
		},
	}
}

func (s *squadHTTPStore) GetAgentInScope(_ context.Context, scope *string, id string) (store.Agent, error) {
	agent, ok := s.agents[id]
	if !ok || (agent.ProjectID != nil && (scope == nil || *agent.ProjectID != *scope)) {
		return store.Agent{}, store.ErrNotFound
	}
	return agent, nil
}

func cloneHTTPSquad(value store.Squad) store.Squad {
	value.Members = append([]store.SquadMember(nil), value.Members...)
	return value
}

func (s *squadHTTPStore) CreateSquad(_ context.Context, input store.Squad) (store.Squad, error) {
	input.ID = squadID
	s.squads[input.ProjectID+":"+input.ID] = cloneHTTPSquad(input)
	return cloneHTTPSquad(input), nil
}

func (s *squadHTTPStore) GetSquad(_ context.Context, projectID, id string) (store.Squad, error) {
	value, ok := s.squads[projectID+":"+id]
	if !ok {
		return store.Squad{}, store.ErrNotFound
	}
	return cloneHTTPSquad(value), nil
}

func (s *squadHTTPStore) ListSquads(_ context.Context, projectID string) ([]store.Squad, error) {
	out := make([]store.Squad, 0)
	for _, value := range s.squads {
		if value.ProjectID == projectID {
			out = append(out, cloneHTTPSquad(value))
		}
	}
	return out, nil
}

func (s *squadHTTPStore) UpdateSquad(_ context.Context, input store.Squad) (store.Squad, error) {
	key := input.ProjectID + ":" + input.ID
	if _, ok := s.squads[key]; !ok {
		return store.Squad{}, store.ErrNotFound
	}
	s.squads[key] = cloneHTTPSquad(input)
	return cloneHTTPSquad(input), nil
}

func (s *squadHTTPStore) DeleteSquad(_ context.Context, projectID, id string) error {
	key := projectID + ":" + id
	if _, ok := s.squads[key]; !ok {
		return store.ErrNotFound
	}
	delete(s.squads, key)
	return nil
}

func newSquadHTTPHandler(t *testing.T) (http.Handler, *projectAccessHTTPStore) {
	t.Helper()
	controlStore := newSquadHTTPStore()
	controlPlane := app.New(controlStore)
	accessStore := newProjectAccessHTTPStore()
	accessService, err := app.NewProjectAccessService(controlPlane, accessStore)
	if err != nil {
		t.Fatal(err)
	}
	a := &api{service: controlPlane, projectAccess: accessService}
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) { a.registerSquadRoutes(r) })
	return router, accessStore
}

func squadRequestWithActor(t *testing.T, handler http.Handler, actor app.AuthenticatedUser, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(context.WithValue(req.Context(), projectActorContextKey{}, actor))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func TestSquadHTTPCRUDUsesProjectAccessAndFullReplacement(t *testing.T) {
	handler, access := newSquadHTTPHandler(t)
	admin := app.AuthenticatedUser{ID: "admin", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	viewer := app.AuthenticatedUser{ID: "viewer", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	member := app.AuthenticatedUser{ID: "member", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	access.roles[projectGrantKey(projectID, admin.ID)] = store.ProjectRoleAdmin
	access.roles[projectGrantKey(projectID, viewer.ID)] = store.ProjectRoleViewer
	access.roles[projectGrantKey(projectID, member.ID)] = store.ProjectRoleMember

	create := squadRequestWithActor(t, handler, admin, http.MethodPost, "/api/projects/"+projectID+"/squads", `{"name":"Core","leaderAgentId":"`+agentID+`","members":[{"agentId":"`+squadMemberAgentID+`","role":"review"}]}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created squadResponse
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID != squadID || created.LeaderAgentID != agentID || len(created.Members) != 1 || created.Members[0].AgentID != squadMemberAgentID {
		t.Fatalf("created squad=%+v", created)
	}

	list := squadRequestWithActor(t, handler, viewer, http.MethodGet, "/api/projects/"+projectID+"/squads", "")
	if list.Code != http.StatusOK {
		t.Fatalf("viewer list status=%d body=%s", list.Code, list.Body.String())
	}
	forbidden := squadRequestWithActor(t, handler, member, http.MethodPost, "/api/projects/"+projectID+"/squads", `{"name":"Nope","leaderAgentId":"`+agentID+`","members":[]}`)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("member create status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	update := squadRequestWithActor(t, handler, admin, http.MethodPut, "/api/projects/"+projectID+"/squads/"+squadID, `{"name":"Core 2","leaderAgentId":"`+squadMemberAgentID+`","members":[{"agentId":"`+agentID+`"}]}`)
	if update.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}
	var updated squadResponse
	if err := json.Unmarshal(update.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Core 2" || updated.LeaderAgentID != squadMemberAgentID || len(updated.Members) != 1 || updated.Members[0].AgentID != agentID {
		t.Fatalf("updated squad=%+v", updated)
	}

	remove := squadRequestWithActor(t, handler, admin, http.MethodDelete, "/api/projects/"+projectID+"/squads/"+squadID, "")
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body.String())
	}
	missing := squadRequestWithActor(t, handler, viewer, http.MethodGet, "/api/projects/"+projectID+"/squads/"+squadID, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted get status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestSquadHTTPRejectsMalformedIdentifiersAndUnknownFields(t *testing.T) {
	handler, access := newSquadHTTPHandler(t)
	admin := app.AuthenticatedUser{ID: "admin", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	access.roles[projectGrantKey(projectID, admin.ID)] = store.ProjectRoleAdmin

	badID := squadRequestWithActor(t, handler, admin, http.MethodGet, "/api/projects/"+projectID+"/squads/not-a-uuid", "")
	if badID.Code != http.StatusBadRequest {
		t.Fatalf("bad id status=%d body=%s", badID.Code, badID.Body.String())
	}
	badBody := squadRequestWithActor(t, handler, admin, http.MethodPost, "/api/projects/"+projectID+"/squads", `{"name":"Core","leaderAgentId":"`+agentID+`","extra":true}`)
	if badBody.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", badBody.Code, badBody.Body.String())
	}
}
