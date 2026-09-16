package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type squadServiceStore struct {
	store.ControlPlaneStore
	projects map[string]store.Project
	agents   map[string]store.Agent
	squads   map[string]store.Squad
	nextID   int
}

func newSquadServiceStore() *squadServiceStore {
	return &squadServiceStore{
		projects: make(map[string]store.Project),
		agents:   make(map[string]store.Agent),
		squads:   make(map[string]store.Squad),
	}
}

func squadStoreKey(projectID, squadID string) string { return projectID + ":" + squadID }

func (f *squadServiceStore) GetProject(_ context.Context, id string) (store.Project, error) {
	project, ok := f.projects[id]
	if !ok {
		return store.Project{}, store.ErrNotFound
	}
	return project, nil
}

func (f *squadServiceStore) GetAgentInScope(_ context.Context, scope *string, id string) (store.Agent, error) {
	agent, ok := f.agents[id]
	if !ok {
		return store.Agent{}, store.ErrNotFound
	}
	if scope == nil {
		if agent.ProjectID != nil {
			return store.Agent{}, store.ErrNotFound
		}
		return agent, nil
	}
	if agent.ProjectID != nil && *agent.ProjectID != *scope {
		return store.Agent{}, store.ErrNotFound
	}
	return agent, nil
}

func cloneSquad(value store.Squad) store.Squad {
	value.Members = append([]store.SquadMember(nil), value.Members...)
	return value
}

func (f *squadServiceStore) CreateSquad(_ context.Context, input store.Squad) (store.Squad, error) {
	f.nextID++
	input.ID = fmt.Sprintf("squad-%d", f.nextID)
	value := cloneSquad(input)
	f.squads[squadStoreKey(input.ProjectID, input.ID)] = value
	return cloneSquad(value), nil
}

func (f *squadServiceStore) GetSquad(_ context.Context, projectID, squadID string) (store.Squad, error) {
	value, ok := f.squads[squadStoreKey(projectID, squadID)]
	if !ok {
		return store.Squad{}, store.ErrNotFound
	}
	return cloneSquad(value), nil
}

func (f *squadServiceStore) ListSquads(_ context.Context, projectID string) ([]store.Squad, error) {
	values := make([]store.Squad, 0)
	for _, value := range f.squads {
		if value.ProjectID == projectID {
			values = append(values, cloneSquad(value))
		}
	}
	return values, nil
}

func (f *squadServiceStore) UpdateSquad(_ context.Context, input store.Squad) (store.Squad, error) {
	key := squadStoreKey(input.ProjectID, input.ID)
	if _, ok := f.squads[key]; !ok {
		return store.Squad{}, store.ErrNotFound
	}
	value := cloneSquad(input)
	f.squads[key] = value
	return cloneSquad(value), nil
}

func (f *squadServiceStore) DeleteSquad(_ context.Context, projectID, squadID string) error {
	key := squadStoreKey(projectID, squadID)
	if _, ok := f.squads[key]; !ok {
		return store.ErrNotFound
	}
	delete(f.squads, key)
	return nil
}

func squadErrorCode(t *testing.T, err error) string {
	t.Helper()
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected application error, got %v", err)
	}
	return apiErr.Code
}

func TestSquadServiceCreateUsesVisibleEnabledAgentsAndNormalizesInput(t *testing.T) {
	projectID := "project-1"
	otherProjectID := "project-2"
	fake := newSquadServiceStore()
	fake.projects[projectID] = store.Project{ID: projectID}
	fake.projects[otherProjectID] = store.Project{ID: otherProjectID}
	fake.agents["leader"] = store.Agent{ID: "leader", ProjectID: &projectID, State: "ENABLED"}
	fake.agents["member"] = store.Agent{ID: "member", ProjectID: &projectID, State: "ENABLED"}
	fake.agents["global"] = store.Agent{ID: "global", State: "ENABLED"}
	fake.agents["other"] = store.Agent{ID: "other", ProjectID: &otherProjectID, State: "ENABLED"}
	fake.agents["disabled"] = store.Agent{ID: "disabled", ProjectID: &projectID, State: "DISABLED"}
	service := New(fake)
	role := "  reviewer  "
	emptyRole := "  "

	created, err := service.CreateSquad(t.Context(), store.Squad{
		ProjectID:     "  " + projectID + "  ",
		Name:          "  Core Team  ",
		LeaderAgentID: " leader ",
		Members: []store.SquadMember{
			{AgentID: " member ", Role: &role},
			{AgentID: "global", Role: &emptyRole},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Core Team" || created.ProjectID != projectID || created.LeaderAgentID != "leader" || len(created.Members) != 2 {
		t.Fatalf("created squad = %+v", created)
	}
	if created.Members[0].Role == nil || *created.Members[0].Role != "reviewer" || created.Members[1].Role != nil {
		t.Fatalf("normalized members = %+v", created.Members)
	}

	for name, input := range map[string]store.Squad{
		"leader duplicated": {ProjectID: projectID, Name: "x", LeaderAgentID: "leader", Members: []store.SquadMember{{AgentID: "leader"}}},
		"member duplicated": {ProjectID: projectID, Name: "x", LeaderAgentID: "leader", Members: []store.SquadMember{{AgentID: "member"}, {AgentID: "member"}}},
	} {
		if _, err := service.CreateSquad(t.Context(), input); err == nil || squadErrorCode(t, err) != "invalid_argument" {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	if _, err := service.CreateSquad(t.Context(), store.Squad{ProjectID: projectID, Name: "cross", LeaderAgentID: "other"}); err == nil || squadErrorCode(t, err) != "agent_not_found" {
		t.Fatalf("cross-project agent error = %v", err)
	}
	if _, err := service.CreateSquad(t.Context(), store.Squad{ProjectID: projectID, Name: "disabled", LeaderAgentID: "disabled"}); err == nil || squadErrorCode(t, err) != "agent_unavailable" {
		t.Fatalf("disabled leader error = %v", err)
	}
}

func TestSquadServiceUpdatePreservesExistingDisabledReferences(t *testing.T) {
	projectID := "project-1"
	fake := newSquadServiceStore()
	fake.projects[projectID] = store.Project{ID: projectID}
	fake.agents["leader"] = store.Agent{ID: "leader", ProjectID: &projectID, State: "ENABLED"}
	fake.agents["member"] = store.Agent{ID: "member", ProjectID: &projectID, State: "ENABLED"}
	fake.agents["new-disabled"] = store.Agent{ID: "new-disabled", ProjectID: &projectID, State: "DISABLED"}
	service := New(fake)

	created, err := service.CreateSquad(t.Context(), store.Squad{
		ProjectID:     projectID,
		Name:          "Core",
		LeaderAgentID: "leader",
		Members:       []store.SquadMember{{AgentID: "member"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	leader := fake.agents["leader"]
	leader.State = "DISABLED"
	fake.agents["leader"] = leader
	member := fake.agents["member"]
	member.State = "DISABLED"
	fake.agents["member"] = member
	role := "legacy context"

	updated, err := service.UpdateSquad(t.Context(), store.Squad{
		ID:            created.ID,
		ProjectID:     projectID,
		Name:          "Renamed",
		LeaderAgentID: "leader",
		Members:       []store.SquadMember{{AgentID: "member", Role: &role}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LeaderAgentID != "leader" || len(updated.Members) != 1 || updated.Members[0].AgentID != "member" {
		t.Fatalf("updated squad = %+v", updated)
	}
	if _, err := service.UpdateSquad(t.Context(), store.Squad{
		ID:            created.ID,
		ProjectID:     projectID,
		Name:          "Bad add",
		LeaderAgentID: "leader",
		Members:       []store.SquadMember{{AgentID: "member"}, {AgentID: "new-disabled"}},
	}); err == nil || squadErrorCode(t, err) != "agent_unavailable" {
		t.Fatalf("new disabled member error = %v", err)
	}
	if _, err := service.UpdateSquad(t.Context(), store.Squad{
		ID:            created.ID,
		ProjectID:     projectID,
		Name:          "Bad leader",
		LeaderAgentID: "member",
	}); err == nil || squadErrorCode(t, err) != "agent_unavailable" {
		t.Fatalf("disabled promoted leader error = %v", err)
	}
}

func TestSquadServiceCRUDAndStoreAvailabilityErrors(t *testing.T) {
	projectID := "project-1"
	fake := newSquadServiceStore()
	fake.projects[projectID] = store.Project{ID: projectID}
	fake.agents["leader"] = store.Agent{ID: "leader", ProjectID: &projectID, State: "ENABLED"}
	service := New(fake)
	created, err := service.CreateSquad(t.Context(), store.Squad{ProjectID: projectID, Name: "Core", LeaderAgentID: "leader"})
	if err != nil {
		t.Fatal(err)
	}
	if values, err := service.ListSquads(t.Context(), projectID); err != nil || len(values) != 1 {
		t.Fatalf("list=%+v err=%v", values, err)
	}
	if got, err := service.GetSquad(t.Context(), projectID, created.ID); err != nil || got.ID != created.ID {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	if err := service.DeleteSquad(t.Context(), projectID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetSquad(t.Context(), projectID, created.ID); err == nil || squadErrorCode(t, err) != "squad_not_found" {
		t.Fatalf("deleted get error = %v", err)
	}

	withoutSquads := New(&squadUnsupportedStore{project: store.Project{ID: projectID}})
	if _, err := withoutSquads.ListSquads(t.Context(), projectID); err == nil || squadErrorCode(t, err) != "squad_management_unavailable" {
		t.Fatalf("unavailable store error = %v", err)
	}
}

type squadUnsupportedStore struct {
	store.ControlPlaneStore
	project store.Project
}

func (f *squadUnsupportedStore) GetProject(_ context.Context, id string) (store.Project, error) {
	if f.project.ID != id {
		return store.Project{}, store.ErrNotFound
	}
	return f.project, nil
}

type squadProjectAccessStore struct {
	*squadServiceStore
	store.ProjectAccessStore
	roles map[string]string
}

func (f *squadProjectAccessStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	role, ok := f.roles[projectID+":"+userID]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func TestProjectAccessSquadsUseReadAndAdministrationBoundaries(t *testing.T) {
	projectID := "project-1"
	base := newSquadServiceStore()
	base.projects[projectID] = store.Project{ID: projectID}
	base.agents["leader"] = store.Agent{ID: "leader", ProjectID: &projectID, State: "ENABLED"}
	fake := &squadProjectAccessStore{
		squadServiceStore: base,
		roles: map[string]string{
			projectID + ":viewer": store.ProjectRoleViewer,
			projectID + ":admin":  store.ProjectRoleAdmin,
		},
	}
	service, err := NewProjectAccessService(New(fake), fake)
	if err != nil {
		t.Fatal(err)
	}
	viewer := AuthenticatedUser{ID: "viewer", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	admin := AuthenticatedUser{ID: "admin", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}

	if values, err := service.ListSquads(t.Context(), viewer, projectID); err != nil || len(values) != 0 {
		t.Fatalf("viewer list=%+v err=%v", values, err)
	}
	if _, err := service.CreateSquad(t.Context(), viewer, store.Squad{ProjectID: projectID, Name: "Nope", LeaderAgentID: "leader"}); err == nil || squadErrorCode(t, err) != "forbidden" {
		t.Fatalf("viewer create error = %v", err)
	}
	created, err := service.CreateSquad(t.Context(), admin, store.Squad{ProjectID: projectID, Name: "Core", LeaderAgentID: "leader"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetSquad(t.Context(), viewer, projectID, created.ID); err != nil {
		t.Fatalf("viewer get error = %v", err)
	}
	if err := service.DeleteSquad(t.Context(), admin, projectID, created.ID); err != nil {
		t.Fatal(err)
	}
}
