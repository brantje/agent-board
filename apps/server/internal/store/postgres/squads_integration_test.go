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
	role := "API implementation"

	created, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     projectA.ID,
		Name:          "Backend",
		LeaderAgentID: agentsA[0].ID,
		Members: []store.SquadMember{
			{AgentID: agentsA[1].ID, Role: &role},
			{AgentID: agentsA[2].ID},
		},
	})
	if err != nil {
		t.Fatalf("create squad: %v", err)
	}
	if created.ID == "" || created.ProjectID != projectA.ID || created.LeaderAgentID != agentsA[0].ID {
		t.Fatalf("unexpected created squad: %+v", created)
	}
	if len(created.Members) != 2 {
		t.Fatalf("created members = %d, want 2", len(created.Members))
	}

	got, err := s.GetSquad(ctx, projectA.ID, created.ID)
	if err != nil {
		t.Fatalf("get squad: %v", err)
	}
	assertSquadMember(t, got.Members, agentsA[1].ID, &role)
	assertSquadMember(t, got.Members, agentsA[2].ID, nil)

	listed, err := s.ListSquads(ctx, projectA.ID)
	if err != nil {
		t.Fatalf("list squads: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("unexpected squad list: %+v", listed)
	}
	if _, err := s.GetSquad(ctx, projectB.ID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project get error = %v, want ErrNotFound", err)
	}

	updatedRole := "Architecture"
	updated, err := s.UpdateSquad(ctx, store.Squad{
		ID:            created.ID,
		ProjectID:     projectA.ID,
		Name:          "Platform",
		LeaderAgentID: agentsA[1].ID,
		Members: []store.SquadMember{
			{AgentID: agentsA[0].ID, Role: &updatedRole},
		},
	})
	if err != nil {
		t.Fatalf("update squad: %v", err)
	}
	if updated.Name != "Platform" || updated.LeaderAgentID != agentsA[1].ID {
		t.Fatalf("unexpected updated squad: %+v", updated)
	}
	assertSquadMember(t, updated.Members, agentsA[0].ID, &updatedRole)

	if err := s.DeleteSquad(ctx, projectA.ID, created.ID); err != nil {
		t.Fatalf("delete squad: %v", err)
	}
	if _, err := s.GetSquad(ctx, projectA.ID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get deleted squad error = %v, want ErrNotFound", err)
	}
}

func TestSquadStoreRejectsInvalidMembership(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-invalid-a", 2)
	_, agentsB := createSquadProjectWithAgents(t, s, "squad-invalid-b", 1)

	if _, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: projectA.ID, Name: "Wrong leader", LeaderAgentID: agentsB[0].ID,
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("cross-project leader error = %v, want ErrInvalidArgument", err)
	}
	if _, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: projectA.ID, Name: "Wrong member", LeaderAgentID: agentsA[0].ID,
		Members: []store.SquadMember{{AgentID: agentsB[0].ID}},
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("cross-project member error = %v, want ErrInvalidArgument", err)
	}
	if _, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: projectA.ID, Name: "Duplicate leader", LeaderAgentID: agentsA[0].ID,
		Members: []store.SquadMember{{AgentID: agentsA[0].ID}},
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("leader-as-member error = %v, want ErrInvalidArgument", err)
	}
	if _, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: projectA.ID, Name: "Duplicate member", LeaderAgentID: agentsA[0].ID,
		Members: []store.SquadMember{{AgentID: agentsA[1].ID}, {AgentID: agentsA[1].ID}},
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate member error = %v, want ErrConflict", err)
	}
}

func TestSquadStoreFailedUpdateRollsBackAndAgentReferenceIsStable(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-rollback-a", 2)
	_, agentsB := createSquadProjectWithAgents(t, s, "squad-rollback-b", 1)
	originalRole := "Worker"
	created, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: projectA.ID, Name: "Stable", LeaderAgentID: agentsA[0].ID,
		Members: []store.SquadMember{{AgentID: agentsA[1].ID, Role: &originalRole}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.UpdateSquad(ctx, store.Squad{
		ID: created.ID, ProjectID: projectA.ID, Name: "Should roll back", LeaderAgentID: agentsA[1].ID,
		Members: []store.SquadMember{{AgentID: agentsB[0].ID}},
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid update error = %v, want ErrInvalidArgument", err)
	}

	got, err := s.GetSquad(ctx, projectA.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Stable" || got.LeaderAgentID != agentsA[0].ID {
		t.Fatalf("failed update changed squad: %+v", got)
	}
	assertSquadMember(t, got.Members, agentsA[1].ID, &originalRole)

	disabled := agentsA[0]
	disabled.State = "DISABLED"
	if _, err := s.UpdateAgent(ctx, &projectA.ID, disabled); err != nil {
		t.Fatalf("disable referenced agent: %v", err)
	}
	got, err = s.GetSquad(ctx, projectA.ID, created.ID)
	if err != nil || got.LeaderAgentID != disabled.ID {
		t.Fatalf("disabled leader reference not preserved: squad=%+v err=%v", got, err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM agents WHERE id = $1`, agentsA[1].ID); err == nil {
		t.Fatal("expected referenced member Agent deletion to be restricted")
	}
}

func createSquadProjectWithAgents(t *testing.T, s *Store, name string, count int) (store.Project, []store.Agent) {
	t.Helper()
	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput(name, "/tmp/"+name, ""))
	if err != nil {
		t.Fatalf("create project %s: %v", name, err)
	}
	provider, err := s.CreateProvider(ctx, store.Provider{
		ProjectID: &project.ID, Name: name + "-provider", Kind: "test", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID: &project.ID, ProviderID: provider.ID, Name: name + "-model", Model: "test", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create model profile: %v", err)
	}
	agents := make([]store.Agent, 0, count)
	for i := 0; i < count; i++ {
		agent, err := s.CreateAgent(ctx, store.Agent{
			ProjectID: &project.ID, Name: name + "-agent-" + string(rune('a'+i)), Engine: "test",
			ModelProfileID: model.ID, EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED",
		})
		if err != nil {
			t.Fatalf("create agent %d: %v", i, err)
		}
		agents = append(agents, agent)
	}
	return project, agents
}

func assertSquadMember(t *testing.T, members []store.SquadMember, agentID string, role *string) {
	t.Helper()
	for _, member := range members {
		if member.AgentID != agentID {
			continue
		}
		if role == nil {
			if member.Role != nil {
				t.Fatalf("member %s role = %q, want nil", agentID, *member.Role)
			}
			return
		}
		if member.Role == nil || *member.Role != *role {
			t.Fatalf("member %s role = %v, want %q", agentID, member.Role, *role)
		}
		return
	}
	t.Fatalf("member %s not found in %+v", agentID, members)
}
