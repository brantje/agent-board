package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadLeaderAndMembershipSerializeOnSquadRow(t *testing.T) {
	t.Run("leader change first", func(t *testing.T) {
		pool := testPool(t)
		s := New(pool)
		ctx := context.Background()

		project, agents := createSquadProjectWithAgents(t, s, "squad-leader-lock-first", 2)
		squad, err := s.CreateSquad(ctx, store.Squad{
			ProjectID:     project.ID,
			Name:          "Locked leader",
			LeaderAgentID: agents[0].ID,
		})
		if err != nil {
			t.Fatal(err)
		}

		leaderTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer leaderTx.Rollback(ctx) //nolint:errcheck
		if _, err := leaderTx.Exec(ctx, `UPDATE squads SET leader_agent_id = $1 WHERE id = $2`, agents[1].ID, squad.ID); err != nil {
			t.Fatalf("stage leader change: %v", err)
		}

		memberTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer memberTx.Rollback(ctx) //nolint:errcheck
		if _, err := memberTx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
			t.Fatal(err)
		}
		_, err = memberTx.Exec(ctx, `INSERT INTO squad_members (squad_id, agent_id) VALUES ($1, $2)`, squad.ID, agents[1].ID)
		assertSquadPostgresCode(t, err, "55P03")
		if err := memberTx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		if err := leaderTx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO squad_members (squad_id, agent_id) VALUES ($1, $2)`, squad.ID, agents[1].ID)
		assertSquadPostgresCode(t, err, "23514")
	})

	t.Run("member insert first", func(t *testing.T) {
		pool := testPool(t)
		s := New(pool)
		ctx := context.Background()

		project, agents := createSquadProjectWithAgents(t, s, "squad-member-lock-first", 2)
		squad, err := s.CreateSquad(ctx, store.Squad{
			ProjectID:     project.ID,
			Name:          "Locked member",
			LeaderAgentID: agents[0].ID,
		})
		if err != nil {
			t.Fatal(err)
		}

		memberTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer memberTx.Rollback(ctx) //nolint:errcheck
		if _, err := memberTx.Exec(ctx, `INSERT INTO squad_members (squad_id, agent_id) VALUES ($1, $2)`, squad.ID, agents[1].ID); err != nil {
			t.Fatalf("stage member insert: %v", err)
		}

		leaderTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer leaderTx.Rollback(ctx) //nolint:errcheck
		if _, err := leaderTx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
			t.Fatal(err)
		}
		_, err = leaderTx.Exec(ctx, `UPDATE squads SET leader_agent_id = $1 WHERE id = $2`, agents[1].ID, squad.ID)
		assertSquadPostgresCode(t, err, "55P03")
		if err := leaderTx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		if err := memberTx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `UPDATE squads SET leader_agent_id = $1 WHERE id = $2`, agents[1].ID, squad.ID)
		assertSquadPostgresCode(t, err, "23514")
	})
}

func TestSquadProjectCannotBeReassigned(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	projectA, members := createSquadProjectWithAgents(t, s, "squad-project-immutable-a", 1)
	projectB, _ := createSquadProjectWithAgents(t, s, "squad-project-immutable-b", 1)
	leader := createGlobalSquadInvariantAgent(t, s, "squad-project-immutable")
	squad, err := s.CreateSquad(ctx, store.Squad{
		ProjectID:     projectA.ID,
		Name:          "Immutable project",
		LeaderAgentID: leader.ID,
		Members:       []store.SquadMember{{AgentID: members[0].ID}},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `UPDATE squads SET project_id = $1 WHERE id = $2`, projectB.ID, squad.ID)
	assertSquadPostgresCode(t, err, "55000")

	got, err := s.GetSquad(ctx, projectA.ID, squad.ID)
	if err != nil {
		t.Fatalf("get original Squad: %v", err)
	}
	if got.ProjectID != projectA.ID {
		t.Fatalf("project id = %q, want %q", got.ProjectID, projectA.ID)
	}
	assertSquadMember(t, got.Members, members[0].ID, nil)
}

func createGlobalSquadInvariantAgent(t *testing.T, s *Store, name string) store.Agent {
	t.Helper()
	ctx := context.Background()
	provider, err := s.CreateProvider(ctx, store.Provider{Name: name + "-provider", Kind: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create global provider: %v", err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{
		ProviderID: provider.ID,
		Name:       name + "-model",
		Model:      "test",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create global model profile: %v", err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{
		Name:             name + "-agent",
		Engine:           "test",
		ModelProfileID:   model.ID,
		EngineSettings:   store.EmptyObject,
		ConcurrencyLimit: 1,
		State:            "ENABLED",
	})
	if err != nil {
		t.Fatalf("create global agent: %v", err)
	}
	return agent
}
