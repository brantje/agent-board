package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	squadHTTPProjectID = "11111111-1111-4111-8111-111111111111"
	squadHTTPLeaderID  = "22222222-2222-4222-8222-222222222222"
	squadHTTPMemberID  = "33333333-3333-4333-8333-333333333333"
	squadHTTPSecondID  = "44444444-4444-4444-8444-444444444444"
	squadHTTPID        = "55555555-5555-4555-8555-555555555555"
	squadHTTPUserID    = "77777777-7777-4777-8777-777777777777"
)

type squadHTTPStore struct {
	store.ControlPlaneStore
	store.ProjectAccessStore
	project store.Project
	roles   map[string]string
	agents  map[string]store.Agent
	squads  map[string]store.Squad
}

func newSquadHTTPStore() *squadHTTPStore {
	projectID := squadHTTPProjectID
	return &squadHTTPStore{
		project: store.Project{ID: projectID, Name: "Project"},
		roles: map[string]string{
			"admin":  store.ProjectRoleAdmin,
			"viewer": store.ProjectRoleViewer,
		},
		agents: map[string]store.Agent{
			squadHTTPLeaderID: {ID: squadHTTPLeaderID, ProjectID: &projectID, State: "ENABLED"},
			squadHTTPMemberID: {ID: squadHTTPMemberID, ProjectID: &projectID, State: "ENABLED"},
			squadHTTPSecondID: {ID: squadHTTPSecondID, ProjectID: nil, State: "ENABLED"},
		},
		squads: make(map[string]store.Squad),
	}
}

func (s *squadHTTPStore) GetProject(_ context.Context, id string) (store.Project, error) {
	if id != s.project.ID {
		return store.Project{}, store.ErrNotFound
	}
	return s.project, nil
}

func (s *squadHTTPStore) GetAgentInScope(_ context.Context, scope *string, id string) (store.Agent, error) {
	agent, ok := s.agents[id]
	if !ok || scope == nil || (agent.ProjectID != nil && *agent.ProjectID != *scope) {
		return store.Agent{}, store.ErrNotFound
	}
	return agent, nil
}

func (s *squadHTTPStore) ValidateProjectWorkflowUser(_ context.Context, projectID, userID string) error {
	if projectID == s.project.ID && userID == squadHTTPUserID {
		return nil
	}
	return store.ErrInvalidArgument
}

func (s *squadHTTPStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	if projectID != s.project.ID {
		return "", store.ErrNotFound
	}
	role, ok := s.roles[userID]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func (s *squadHTTPStore) CreateSquad(_ context.Context, value store.Squad) (store.Squad, error) {
	value.ID = squadHTTPID
	value.CreatedAt = time.Unix(1, 0).UTC()
	value.UpdatedAt = value.CreatedAt
	s.squads[value.ID] = value
	return value, nil
}

func (s *squadHTTPStore) GetSquad(_ context.Context, projectID, id string) (store.Squad, error) {
	value, ok := s.squads[id]
	if !ok || value.ProjectID != projectID {
		return store.Squad{}, store.ErrNotFound
	}
	return value, nil
}

func (s *squadHTTPStore) ListSquads(_ context.Context, projectID string) ([]store.Squad, error) {
	values := make([]store.Squad, 0, len(s.squads))
	for _, value := range s.squads {
		if value.ProjectID == projectID {
			values = append(values, value)
		}
	}
	return values, nil
}

func (s *squadHTTPStore) UpdateSquad(_ context.Context, value store.Squad) (store.SquadUpdateResult, error) {
	previous, ok := s.squads[value.ID]
	if !ok || previous.ProjectID != value.ProjectID {
		return store.SquadUpdateResult{}, store.ErrNotFound
	}
	leaderChanged := previous.LeaderAgentID != value.LeaderAgentID
	value.CreatedAt = previous.CreatedAt
	value.UpdatedAt = time.Unix(2, 0).UTC()
	s.squads[value.ID] = value
	return store.SquadUpdateResult{Squad: value, LeaderChanged: leaderChanged}, nil
}

func (s *squadHTTPStore) DeleteSquad(_ context.Context, projectID, id string) error {
	value, ok := s.squads[id]
	if !ok || value.ProjectID != projectID {
		return store.ErrNotFound
	}
	delete(s.squads, id)
	return nil
}

func squadHTTPRouter(t *testing.T, fake *squadHTTPStore, actorID string) http.Handler {
	t.Helper()
	controlPlane := app.New(fake)
	access, err := app.NewProjectAccessService(controlPlane, fake)
	if err != nil {
		t.Fatal(err)
	}
	a := &api{projectAccess: access}
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor := app.AuthenticatedUser{ID: actorID, DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), projectActorContextKey{}, actor)))
		})
	})
	a.registerSquadRoutes(router)
	return router
}

func squadHTTPRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func TestSquadHTTPCRUDAndMixedMemberRoundTrip(t *testing.T) {
	fake := newSquadHTTPStore()
	router := squadHTTPRouter(t, fake, "admin")

	created := squadHTTPRequest(t, router, http.MethodPost, "/projects/"+squadHTTPProjectID+"/squads", map[string]any{
		"name": "  Core  ", "leaderAgentId": squadHTTPLeaderID,
		"members": []map[string]any{
			{"type": "AGENT", "id": squadHTTPMemberID, "role": "  reviewer  "},
			{"type": "USER", "id": squadHTTPUserID, "role": "  product  "},
		},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var got squadDTO
	if err := json.Unmarshal(created.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != squadHTTPID || got.Name != "Core" || got.LeaderAgentID != squadHTTPLeaderID || len(got.Members) != 2 {
		t.Fatalf("created=%+v", got)
	}
	if got.Members[0].Type != "AGENT" || got.Members[0].ID != squadHTTPMemberID || got.Members[0].Role == nil || *got.Members[0].Role != "reviewer" {
		t.Fatalf("Agent member=%+v", got.Members[0])
	}
	if got.Members[1].Type != "USER" || got.Members[1].ID != squadHTTPUserID || got.Members[1].Role == nil || *got.Members[1].Role != "product" {
		t.Fatalf("User member=%+v", got.Members[1])
	}

	listed := squadHTTPRequest(t, router, http.MethodGet, "/projects/"+squadHTTPProjectID+"/squads", nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	var values []squadDTO
	if err := json.Unmarshal(listed.Body.Bytes(), &values); err != nil || len(values) != 1 || len(values[0].Members) != 2 || values[0].Members[1].Type != "USER" {
		t.Fatalf("list=%+v err=%v", values, err)
	}

	updated := squadHTTPRequest(t, router, http.MethodPut, "/projects/"+squadHTTPProjectID+"/squads/"+squadHTTPID, map[string]any{
		"name": "Core 2", "leaderAgentId": squadHTTPSecondID,
		"members": []map[string]any{
			{"type": "AGENT", "id": squadHTTPLeaderID},
			{"type": "USER", "id": squadHTTPUserID, "role": "Product"},
		},
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &got); err != nil || got.LeaderAgentID != squadHTTPSecondID || len(got.Members) != 2 || got.Members[0].Type != "AGENT" || got.Members[0].ID != squadHTTPLeaderID || got.Members[1].Type != "USER" {
		t.Fatalf("updated=%+v err=%v", got, err)
	}

	fetched := squadHTTPRequest(t, router, http.MethodGet, "/projects/"+squadHTTPProjectID+"/squads/"+squadHTTPID, nil)
	if fetched.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", fetched.Code, fetched.Body.String())
	}
	var fetchedSquad squadDTO
	if err := json.Unmarshal(fetched.Body.Bytes(), &fetchedSquad); err != nil || len(fetchedSquad.Members) != 2 || fetchedSquad.Members[1].Type != "USER" || fetchedSquad.Members[1].ID != squadHTTPUserID {
		t.Fatalf("get=%+v err=%v", fetchedSquad, err)
	}

	deleted := squadHTTPRequest(t, router, http.MethodDelete, "/projects/"+squadHTTPProjectID+"/squads/"+squadHTTPID, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestSquadHTTPAuthorizationAndTypedValidation(t *testing.T) {
	fake := newSquadHTTPStore()
	viewer := squadHTTPRouter(t, fake, "viewer")
	admin := squadHTTPRouter(t, fake, "admin")

	read := squadHTTPRequest(t, viewer, http.MethodGet, "/projects/"+squadHTTPProjectID+"/squads", nil)
	if read.Code != http.StatusOK {
		t.Fatalf("viewer read status=%d body=%s", read.Code, read.Body.String())
	}
	denied := squadHTTPRequest(t, viewer, http.MethodPost, "/projects/"+squadHTTPProjectID+"/squads", map[string]any{
		"name": "Nope", "leaderAgentId": squadHTTPLeaderID, "members": []any{},
	})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer mutate status=%d body=%s", denied.Code, denied.Body.String())
	}

	invalidID := squadHTTPRequest(t, admin, http.MethodPost, "/projects/"+squadHTTPProjectID+"/squads", map[string]any{
		"name": "Bad", "leaderAgentId": squadHTTPLeaderID, "members": []map[string]any{{"type": "USER", "id": "not-a-uuid"}},
	})
	if invalidID.Code != http.StatusBadRequest {
		t.Fatalf("invalid member id status=%d body=%s", invalidID.Code, invalidID.Body.String())
	}

	unknownType := squadHTTPRequest(t, admin, http.MethodPost, "/projects/"+squadHTTPProjectID+"/squads", map[string]any{
		"name": "Bad", "leaderAgentId": squadHTTPLeaderID, "members": []map[string]any{{"type": "GROUP", "id": squadHTTPUserID}},
	})
	if unknownType.Code != http.StatusBadRequest {
		t.Fatalf("unknown type status=%d body=%s", unknownType.Code, unknownType.Body.String())
	}

	ineligibleUser := "88888888-8888-4888-8888-888888888888"
	ineligible := squadHTTPRequest(t, admin, http.MethodPost, "/projects/"+squadHTTPProjectID+"/squads", map[string]any{
		"name": "Bad", "leaderAgentId": squadHTTPLeaderID, "members": []map[string]any{{"type": "USER", "id": ineligibleUser}},
	})
	if ineligible.Code != http.StatusBadRequest {
		t.Fatalf("ineligible User status=%d body=%s", ineligible.Code, ineligible.Body.String())
	}

	badID := squadHTTPRequest(t, admin, http.MethodGet, "/projects/"+squadHTTPProjectID+"/squads/not-a-uuid", nil)
	if badID.Code != http.StatusBadRequest {
		t.Fatalf("invalid squad id status=%d body=%s", badID.Code, badID.Body.String())
	}
}
