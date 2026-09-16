package app

import (
	"context"
	"sort"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	squadProjectID      = "11111111-1111-4111-8111-111111111111"
	squadOtherProjectID = "22222222-2222-4222-8222-222222222222"
	squadLeaderID       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	squadMemberID       = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	squadGlobalAgentID  = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	squadID             = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
)

type squadTestStore struct {
	*fakeStore
	agents      map[string]store.Agent
	squads      map[string]store.Squad
	createCalls int
	updateCalls int
}

var _ store.ControlPlaneStore = (*squadTestStore)(nil)

func newSquadTestStore() *squadTestStore {
	projectID := squadProjectID
	otherProjectID := squadOtherProjectID
	return &squadTestStore{
		fakeStore: &fakeStore{project: store.Project{ID: projectID}},
		agents: map[string]store.Agent{
			squadLeaderID:                          {ID: squadLeaderID, ProjectID: &projectID, Name: "Leader", State: "ENABLED"},
			squadMemberID:                          {ID: squadMemberID, ProjectID: &projectID, Name: "Member", State: "ENABLED"},
			squadGlobalAgentID:                     {ID: squadGlobalAgentID, Name: "Global", State: "ENABLED"},
			"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee": {ID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", ProjectID: &otherProjectID, Name: "Other", State: "ENABLED"},
		},
		squads: map[string]store.Squad{},
	}
}

func (s *squadTestStore) GetAgentInScope(_ context.Context, scope *string, id string) (store.Agent, error) {
	agent, ok := s.agents[id]
	if !ok {
		return store.Agent{}, store.ErrNotFound
	}
	if scope != nil && agent.ProjectID != nil && *agent.ProjectID != *scope {
		return store.Agent{}, store.ErrNotFound
	}
	return agent, nil
}

func (s *squadTestStore) CreateSquad(_ context.Context, value store.Squad) (store.Squad, error) {
	s.createCalls++
	for _, existing := range s.squads {
		if existing.ProjectID == value.ProjectID && existing.Name == value.Name {
			return store.Squad{}, store.ErrConflict
		}
	}
	value.ID = squadID
	value.Members = cloneTestSquadMembers(value.Members)
	s.squads[value.ID] = value
	return cloneTestSquad(value), nil
}

func (s *squadTestStore) GetSquad(_ context.Context, projectID, id string) (store.Squad, error) {
	value, ok := s.squads[id]
	if !ok || value.ProjectID != projectID {
		return store.Squad{}, store.ErrNotFound
	}
	return cloneTestSquad(value), nil
}

func (s *squadTestStore) ListSquads(_ context.Context, projectID string) ([]store.Squad, error) {
	values := make([]store.Squad, 0)
	for _, value := range s.squads {
		if value.ProjectID == projectID {
			values = append(values, cloneTestSquad(value))
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, nil
}

func (s *squadTestStore) UpdateSquad(_ context.Context, value store.Squad) (store.Squad, error) {
	s.updateCalls++
	existing, ok := s.squads[value.ID]
	if !ok || existing.ProjectID != value.ProjectID {
		return store.Squad{}, store.ErrNotFound
	}
	value.Members = cloneTestSquadMembers(value.Members)
	s.squads[value.ID] = value
	return cloneTestSquad(value), nil
}

func (s *squadTestStore) DeleteSquad(_ context.Context, projectID, id string) error {
	value, ok := s.squads[id]
	if !ok || value.ProjectID != projectID {
		return store.ErrNotFound
	}
	delete(s.squads, id)
	return nil
}

func cloneTestSquad(value store.Squad) store.Squad {
	value.Members = cloneTestSquadMembers(value.Members)
	return value
}

func cloneTestSquadMembers(values []store.SquadMember) []store.SquadMember {
	result := make([]store.SquadMember, len(values))
	for i, value := range values {
		result[i] = value
		if value.Role != nil {
			role := *value.Role
			result[i].Role = &role
		}
	}
	return result
}

func TestCreateSquadNormalizesAndAcceptsProjectAndGlobalAgents(t *testing.T) {
	ctx := context.Background()
	data := newSquadTestStore()
	svc := New(data)
	role := "  API implementation  "
	emptyRole := "  "

	created, err := svc.CreateSquad(ctx, store.Squad{
		ProjectID:     squadProjectID,
		Name:          "  Backend  ",
		LeaderAgentID: squadLeaderID,
		Members: []store.SquadMember{
			{AgentID: squadMemberID, Role: &role},
			{AgentID: squadGlobalAgentID, Role: &emptyRole},
		},
	})
	if err != nil {
		t.Fatalf("CreateSquad: %v", err)
	}
	if data.createCalls != 1 {
		t.Fatalf("CreateSquad store calls = %d, want 1", data.createCalls)
	}
	if created.Name != "Backend" || created.LeaderAgentID != squadLeaderID {
		t.Fatalf("created squad = %+v", created)
	}
	if len(created.Members) != 2 || created.Members[0].Role == nil || *created.Members[0].Role != "API implementation" {
		t.Fatalf("members = %+v", created.Members)
	}
	if created.Members[1].Role != nil {
		t.Fatalf("blank descriptive role = %v, want nil", created.Members[1].Role)
	}
}

func TestSquadValidationRejectsInvalidMembershipBeforeStore(t *testing.T) {
	otherAgentID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	disabledID := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	ctx := context.Background()

	tests := []struct {
		name  string
		input store.Squad
		setup func(*squadTestStore)
		code  string
	}{
		{name: "missing leader", input: store.Squad{ProjectID: squadProjectID, Name: "Squad"}, code: "invalid_argument"},
		{name: "invalid leader id", input: store.Squad{ProjectID: squadProjectID, Name: "Squad", LeaderAgentID: "not-a-uuid"}, code: "invalid_argument"},
		{name: "leader duplicated as member", input: store.Squad{ProjectID: squadProjectID, Name: "Squad", LeaderAgentID: squadLeaderID, Members: []store.SquadMember{{AgentID: squadLeaderID}}}, code: "invalid_argument"},
		{name: "duplicate member", input: store.Squad{ProjectID: squadProjectID, Name: "Squad", LeaderAgentID: squadLeaderID, Members: []store.SquadMember{{AgentID: squadMemberID}, {AgentID: squadMemberID}}}, code: "invalid_argument"},
		{name: "cross project leader", input: store.Squad{ProjectID: squadProjectID, Name: "Squad", LeaderAgentID: otherAgentID}, code: "agent_not_found"},
		{name: "cross project member", input: store.Squad{ProjectID: squadProjectID, Name: "Squad", LeaderAgentID: squadLeaderID, Members: []store.SquadMember{{AgentID: otherAgentID}}}, code: "agent_not_found"},
		{name: "disabled leader", input: store.Squad{ProjectID: squadProjectID, Name: "Squad", LeaderAgentID: disabledID}, setup: func(data *squadTestStore) {
			projectID := squadProjectID
			data.agents[disabledID] = store.Agent{ID: disabledID, ProjectID: &projectID, State: "DISABLED"}
		}, code: "invalid_argument"},
		{name: "disabled member", input: store.Squad{ProjectID: squadProjectID, Name: "Squad", LeaderAgentID: squadLeaderID, Members: []store.SquadMember{{AgentID: disabledID}}}, setup: func(data *squadTestStore) {
			projectID := squadProjectID
			data.agents[disabledID] = store.Agent{ID: disabledID, ProjectID: &projectID, State: "ARCHIVED"}
		}, code: "invalid_argument"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := newSquadTestStore()
			if tt.setup != nil {
				tt.setup(data)
			}
			svc := New(data)
			_, err := svc.CreateSquad(ctx, tt.input)
			appErr, ok := AsError(err)
			if !ok || appErr.Code != tt.code {
				t.Fatalf("error = %#v, want code %q", err, tt.code)
			}
			if data.createCalls != 0 {
				t.Fatalf("invalid input reached store %d times", data.createCalls)
			}
		})
	}
}

func TestSquadCRUDTranslatesStoreErrorsAndReadsDoNotRevalidateAgents(t *testing.T) {
	ctx := context.Background()
	data := newSquadTestStore()
	data.squads[squadID] = store.Squad{ID: squadID, ProjectID: squadProjectID, Name: "Backend", LeaderAgentID: squadLeaderID}
	leader := data.agents[squadLeaderID]
	leader.State = "DISABLED"
	data.agents[squadLeaderID] = leader
	svc := New(data)

	got, err := svc.GetSquad(ctx, squadProjectID, squadID)
	if err != nil {
		t.Fatalf("GetSquad with disabled persisted leader: %v", err)
	}
	if got.LeaderAgentID != squadLeaderID {
		t.Fatalf("leader = %q", got.LeaderAgentID)
	}
	listed, err := svc.ListSquads(ctx, squadProjectID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListSquads = %+v, %v", listed, err)
	}

	if _, err := svc.GetSquad(ctx, squadProjectID, "not-a-uuid"); !isAppCode(err, "invalid_argument") {
		t.Fatalf("invalid squad id error = %v", err)
	}
	if _, err := svc.GetSquad(ctx, squadProjectID, "99999999-9999-4999-8999-999999999999"); !isAppCode(err, "squad_not_found") {
		t.Fatalf("missing squad error = %v", err)
	}
	if err := svc.DeleteSquad(ctx, squadProjectID, "99999999-9999-4999-8999-999999999999"); !isAppCode(err, "squad_not_found") {
		t.Fatalf("missing delete error = %v", err)
	}
}

func TestUpdateSquadValidatesCompleteDesiredMembershipAndPersistsOnce(t *testing.T) {
	ctx := context.Background()
	data := newSquadTestStore()
	data.squads[squadID] = store.Squad{ID: squadID, ProjectID: squadProjectID, Name: "Backend", LeaderAgentID: squadLeaderID}
	svc := New(data)
	role := "  Reviewer "

	updated, err := svc.UpdateSquad(ctx, store.Squad{
		ID:            squadID,
		ProjectID:     squadProjectID,
		Name:          " Platform ",
		LeaderAgentID: squadMemberID,
		Members:       []store.SquadMember{{AgentID: squadLeaderID, Role: &role}},
	})
	if err != nil {
		t.Fatalf("UpdateSquad: %v", err)
	}
	if data.updateCalls != 1 {
		t.Fatalf("UpdateSquad store calls = %d, want 1", data.updateCalls)
	}
	if updated.Name != "Platform" || updated.LeaderAgentID != squadMemberID || len(updated.Members) != 1 {
		t.Fatalf("updated squad = %+v", updated)
	}
	if updated.Members[0].Role == nil || *updated.Members[0].Role != "Reviewer" {
		t.Fatalf("updated role = %v", updated.Members[0].Role)
	}
}

func isAppCode(err error, code string) bool {
	appErr, ok := AsError(err)
	return ok && appErr.Code == code
}
