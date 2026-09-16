package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadStoreCRUDAndProjectIsolation(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-a", 3)
	projectB, _ := createSquadProjectWithAgents(t, s, "squad-b", 1)
	user := createSquadWorkflowUser(t, s, projectA.ID, "squad-a-user", store.ProjectRoleAdmin, store.DeploymentRoleMember, store.UserStatusActive)
	agentRole := "API implementation"
	userRole := "Product"

	created, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: projectA.ID, Name: "Backend", LeaderAgentID: agentsA[0].ID,
		Members: []store.SquadMember{
			{Type: store.SquadMemberTypeAgent, ID: agentsA[1].ID, Role: &agentRole},
			{Type: store.SquadMemberTypeUser, ID: user.ID, Role: &userRole},
			{Type: store.SquadMemberTypeAgent, ID: agentsA[2].ID},
		},
	})
	if err != nil {
		t.Fatalf("create squad: %v", err)
	}
	if created.ID == "" || created.ProjectID != projectA.ID || created.LeaderAgentID != agentsA[0].ID || len(created.Members) != 3 {
		t.Fatalf("unexpected created squad: %+v", created)
	}

	got, err := s.GetSquad(ctx, projectA.ID, created.ID)
	if err != nil {
		t.Fatalf("get squad: %v", err)
	}
	assertSquadMember(t, got.Members, store.SquadMemberTypeAgent, agentsA[1].ID, &agentRole)
	assertSquadMember(t, got.Members, store.SquadMemberTypeUser, user.ID, &userRole)
	assertSquadMember(t, got.Members, store.SquadMemberTypeAgent, agentsA[2].ID, nil)

	listed, err := s.ListSquads(ctx, projectA.ID)
	if err != nil {
		t.Fatalf("list squads: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("unexpected squad list: %+v", listed)
	}
	assertSquadMember(t, listed[0].Members, store.SquadMemberTypeUser, user.ID, &userRole)
	if _, err := s.GetSquad(ctx, projectB.ID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project get error = %v, want ErrNotFound", err)
	}

	updatedRole := "Architecture"
	result, err := s.UpdateSquad(ctx, store.Squad{
		ID: created.ID, ProjectID: projectA.ID, Name: "Platform", LeaderAgentID: agentsA[1].ID,
		Members: []store.SquadMember{
			{Type: store.SquadMemberTypeAgent, ID: agentsA[0].ID, Role: &updatedRole},
			{Type: store.SquadMemberTypeUser, ID: user.ID},
		},
	})
	if err != nil {
		t.Fatalf("update squad: %v", err)
	}
	if !result.LeaderChanged {
		t.Fatal("leader change was not reported")
	}
	updated := result.Squad
	if updated.Name != "Platform" || updated.LeaderAgentID != agentsA[1].ID {
		t.Fatalf("unexpected updated squad: %+v", updated)
	}
	assertSquadMember(t, updated.Members, store.SquadMemberTypeAgent, agentsA[0].ID, &updatedRole)
	assertSquadMember(t, updated.Members, store.SquadMemberTypeUser, user.ID, nil)
	if len(result.Events) != 1 || result.Events[0].Type != "squad.updated" || result.Events[0].ProjectID != projectA.ID {
		t.Fatalf("update events = %+v", result.Events)
	}
	var persistedEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE project_id=$1 AND type='squad.updated' AND payload->>'squadId'=$2`, projectA.ID, created.ID).Scan(&persistedEvents); err != nil {
		t.Fatalf("count squad.updated events: %v", err)
	}
	if persistedEvents != 1 {
		t.Fatalf("persisted squad.updated events = %d, want 1", persistedEvents)
	}

	if err := s.DeleteSquad(ctx, projectA.ID, created.ID); err != nil {
		t.Fatalf("delete squad: %v", err)
	}
	if _, err := s.GetSquad(ctx, projectA.ID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get deleted squad error = %v, want ErrNotFound", err)
	}
}

func TestSquadStoreRejectsInvalidMembership(t *testing.T) {
	s := New(testPool(t))
	projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-invalid-a", 2)
	_, agentsB := createSquadProjectWithAgents(t, s, "squad-invalid-b", 1)
	agent := func(id string) store.SquadMember { return store.SquadMember{Type: store.SquadMemberTypeAgent, ID: id} }

	if _, err := s.CreateSquad(t.Context(), store.Squad{ProjectID: projectA.ID, Name: "Wrong leader", LeaderAgentID: agentsB[0].ID}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("cross-project leader error = %v, want ErrInvalidArgument", err)
	}
	if _, err := s.CreateSquad(t.Context(), store.Squad{ProjectID: projectA.ID, Name: "Wrong member", LeaderAgentID: agentsA[0].ID, Members: []store.SquadMember{agent(agentsB[0].ID)}}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("cross-project member error = %v, want ErrInvalidArgument", err)
	}
	if _, err := s.CreateSquad(t.Context(), store.Squad{ProjectID: projectA.ID, Name: "Duplicate leader", LeaderAgentID: agentsA[0].ID, Members: []store.SquadMember{agent(agentsA[0].ID)}}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("leader-as-member error = %v, want ErrInvalidArgument", err)
	}
	if _, err := s.CreateSquad(t.Context(), store.Squad{ProjectID: projectA.ID, Name: "Duplicate member", LeaderAgentID: agentsA[0].ID, Members: []store.SquadMember{agent(agentsA[1].ID), agent(agentsA[1].ID)}}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate member error = %v, want ErrConflict", err)
	}
	if _, err := s.CreateSquad(t.Context(), store.Squad{ProjectID: projectA.ID, Name: "Unknown type", LeaderAgentID: agentsA[0].ID, Members: []store.SquadMember{{Type: "GROUP", ID: agentsA[1].ID}}}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("unknown type error = %v, want ErrInvalidArgument", err)
	}
}

func TestSquadStoreFailedUpdateRollsBackAndReferencesRemainStable(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-rollback-a", 2)
	_, agentsB := createSquadProjectWithAgents(t, s, "squad-rollback-b", 1)
	user := createSquadWorkflowUser(t, s, projectA.ID, "squad-rollback-user", store.ProjectRoleAdmin, store.DeploymentRoleMember, store.UserStatusActive)
	originalRole := "Worker"
	created, err := s.CreateSquad(t.Context(), store.Squad{
		ProjectID: projectA.ID, Name: "Stable", LeaderAgentID: agentsA[0].ID,
		Members: []store.SquadMember{
			{Type: store.SquadMemberTypeAgent, ID: agentsA[1].ID, Role: &originalRole},
			{Type: store.SquadMemberTypeUser, ID: user.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.UpdateSquad(t.Context(), store.Squad{
		ID: created.ID, ProjectID: projectA.ID, Name: "Should roll back", LeaderAgentID: agentsA[1].ID,
		Members: []store.SquadMember{{Type: store.SquadMemberTypeAgent, ID: agentsB[0].ID}},
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid update error = %v, want ErrInvalidArgument", err)
	}

	got, err := s.GetSquad(t.Context(), projectA.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Stable" || got.LeaderAgentID != agentsA[0].ID {
		t.Fatalf("failed update changed squad: %+v", got)
	}
	assertSquadMember(t, got.Members, store.SquadMemberTypeAgent, agentsA[1].ID, &originalRole)
	assertSquadMember(t, got.Members, store.SquadMemberTypeUser, user.ID, nil)

	disabled := agentsA[0]
	disabled.State = "DISABLED"
	if _, err := s.UpdateAgent(t.Context(), &projectA.ID, disabled); err != nil {
		t.Fatalf("disable referenced agent: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE project_user_access SET role='viewer' WHERE project_id=$1 AND user_id=$2`, projectA.ID, user.ID); err != nil {
		t.Fatalf("make persisted User membership stale: %v", err)
	}
	got, err = s.GetSquad(t.Context(), projectA.ID, created.ID)
	if err != nil || got.LeaderAgentID != disabled.ID {
		t.Fatalf("stale references not preserved: squad=%+v err=%v", got, err)
	}
	assertSquadMember(t, got.Members, store.SquadMemberTypeUser, user.ID, nil)

	if _, err := pool.Exec(t.Context(), `DELETE FROM agents WHERE id = $1`, agentsA[1].ID); err == nil {
		t.Fatal("expected referenced member Agent deletion to be restricted")
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, user.ID); err == nil {
		t.Fatal("expected referenced member User deletion to be restricted")
	}
}

func createSquadProjectWithAgents(t *testing.T, s *Store, name string, count int) (store.Project, []store.Agent) {
	t.Helper()
	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput(name, "/tmp/"+name, ""))
	if err != nil {
		t.Fatalf("create project %s: %v", name, err)
	}
	provider, err := s.CreateProvider(ctx, store.Provider{ProjectID: &project.ID, Name: name + "-provider", Kind: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{ProjectID: &project.ID, ProviderID: provider.ID, Name: name + "-model", Model: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create model profile: %v", err)
	}
	agents := make([]store.Agent, 0, count)
	for i := 0; i < count; i++ {
		agent, err := s.CreateAgent(ctx, store.Agent{ProjectID: &project.ID, Name: name + "-agent-" + string(rune('a'+i)), Engine: "test", ModelProfileID: model.ID, EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED"})
		if err != nil {
			t.Fatalf("create agent %d: %v", i, err)
		}
		agents = append(agents, agent)
	}
	return project, agents
}

func assertSquadMember(t *testing.T, members []store.SquadMember, memberType, id string, role *string) {
	t.Helper()
	for _, member := range members {
		if member.Type != memberType || member.ID != id {
			continue
		}
		if role == nil {
			if member.Role != nil {
				t.Fatalf("member %s:%s role = %q, want nil", memberType, id, *member.Role)
			}
			return
		}
		if member.Role == nil || *member.Role != *role {
			t.Fatalf("member %s:%s role = %v, want %q", memberType, id, member.Role, *role)
		}
		return
	}
	t.Fatalf("member %s:%s not found in %+v", memberType, id, members)
}
