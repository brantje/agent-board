package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSquadStoreSerializesWithAgentOwnershipChanges(t *testing.T) {
	t.Run("ownership change first", func(t *testing.T) {
		pool := testPool(t)
		s := New(pool)
		ctx := context.Background()

		projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-lock-owner-a", 2)
		projectB, agentsB := createSquadProjectWithAgents(t, s, "squad-lock-owner-b", 1)

		ownerTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer ownerTx.Rollback(ctx) //nolint:errcheck
		if _, err := ownerTx.Exec(ctx, `
			UPDATE agents
			SET project_id = $1, model_profile_id = $2
			WHERE id = $3
		`, projectB.ID, agentsB[0].ModelProfileID, agentsA[1].ID); err != nil {
			t.Fatalf("stage Agent ownership change: %v", err)
		}

		attemptCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err = s.CreateSquad(attemptCtx, store.Squad{
			ProjectID:     projectA.ID,
			Name:          "Blocked create",
			LeaderAgentID: agentsA[0].ID,
			Members:       []store.SquadMember{{Type: store.SquadMemberTypeAgent, ID: agentsA[1].ID}},
		})
		if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled)) {
			t.Fatalf("blocked CreateSquad error = %v, want context timeout", err)
		}

		if err := ownerTx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = s.CreateSquad(ctx, store.Squad{
			ProjectID:     projectA.ID,
			Name:          "Rejected create",
			LeaderAgentID: agentsA[0].ID,
			Members:       []store.SquadMember{{Type: store.SquadMemberTypeAgent, ID: agentsA[1].ID}},
		})
		if !errors.Is(err, store.ErrInvalidArgument) {
			t.Fatalf("cross-Project CreateSquad error = %v, want ErrInvalidArgument", err)
		}
	})

	t.Run("Squad reference first", func(t *testing.T) {
		pool := testPool(t)
		s := New(pool)
		ctx := context.Background()

		projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-lock-ref-a", 2)
		projectB, agentsB := createSquadProjectWithAgents(t, s, "squad-lock-ref-b", 1)
		input := store.Squad{
			ProjectID:     projectA.ID,
			Name:          "Held reference",
			LeaderAgentID: agentsA[0].ID,
			Members:       []store.SquadMember{{Type: store.SquadMemberTypeAgent, ID: agentsA[1].ID}},
		}

		refTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer refTx.Rollback(ctx) //nolint:errcheck
		if err := lockSquadIdentities(ctx, refTx, input); err != nil {
			t.Fatalf("lock Squad identities: %v", err)
		}
		var squadID string
		if err := refTx.QueryRow(ctx, `
			INSERT INTO squads (project_id, name, leader_agent_id)
			VALUES ($1, $2, $3)
			RETURNING id::text
		`, input.ProjectID, input.Name, input.LeaderAgentID).Scan(&squadID); err != nil {
			t.Fatalf("stage Squad: %v", err)
		}
		if _, err := refTx.Exec(ctx, `
			INSERT INTO squad_members (squad_id, agent_id)
			VALUES ($1, $2)
		`, squadID, agentsA[1].ID); err != nil {
			t.Fatalf("stage Squad member: %v", err)
		}

		ownerTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer ownerTx.Rollback(ctx) //nolint:errcheck
		if _, err := ownerTx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
			t.Fatal(err)
		}
		_, err = ownerTx.Exec(ctx, `
			UPDATE agents
			SET project_id = $1, model_profile_id = $2
			WHERE id = $3
		`, projectB.ID, agentsB[0].ModelProfileID, agentsA[1].ID)
		assertSquadPostgresCode(t, err, "55P03")
		if err := ownerTx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		if err := refTx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `
			UPDATE agents
			SET project_id = $1, model_profile_id = $2
			WHERE id = $3
		`, projectB.ID, agentsB[0].ModelProfileID, agentsA[1].ID)
		assertSquadPostgresCode(t, err, "23514")
	})
}

func TestSquadDatabaseSerializesDirectReferencesWithAgentOwnershipChanges(t *testing.T) {
	t.Run("member waits for Project ownership change", func(t *testing.T) {
		pool := testPool(t)
		s := New(pool)
		ctx := context.Background()

		projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-db-lock-member-a", 2)
		projectB, agentsB := createSquadProjectWithAgents(t, s, "squad-db-lock-member-b", 1)
		squad, err := s.CreateSquad(ctx, store.Squad{
			ProjectID:     projectA.ID,
			Name:          "Direct member lock",
			LeaderAgentID: agentsA[0].ID,
		})
		if err != nil {
			t.Fatal(err)
		}

		ownerTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer ownerTx.Rollback(ctx) //nolint:errcheck
		if _, err := ownerTx.Exec(ctx, `
			UPDATE agents
			SET project_id = $1, model_profile_id = $2
			WHERE id = $3
		`, projectB.ID, agentsB[0].ModelProfileID, agentsA[1].ID); err != nil {
			t.Fatalf("stage Agent ownership change: %v", err)
		}

		memberTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer memberTx.Rollback(ctx) //nolint:errcheck
		if _, err := memberTx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
			t.Fatal(err)
		}
		_, err = memberTx.Exec(ctx, `
			INSERT INTO squad_members (squad_id, agent_id)
			VALUES ($1, $2)
		`, squad.ID, agentsA[1].ID)
		assertSquadPostgresCode(t, err, "55P03")
		if err := memberTx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		if err := ownerTx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO squad_members (squad_id, agent_id)
			VALUES ($1, $2)
		`, squad.ID, agentsA[1].ID)
		assertSquadPostgresCode(t, err, "23514")
	})

	t.Run("global leader blocks Project ownership change", func(t *testing.T) {
		pool := testPool(t)
		s := New(pool)
		ctx := context.Background()

		projectA, agentsA := createSquadProjectWithAgents(t, s, "squad-db-lock-leader-a", 1)
		projectB, _ := createSquadProjectWithAgents(t, s, "squad-db-lock-leader-b", 1)
		globalAgent := createGlobalSquadInvariantAgent(t, s, "squad-db-lock-global")
		squad, err := s.CreateSquad(ctx, store.Squad{
			ProjectID:     projectA.ID,
			Name:          "Direct leader lock",
			LeaderAgentID: agentsA[0].ID,
		})
		if err != nil {
			t.Fatal(err)
		}

		refTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer refTx.Rollback(ctx) //nolint:errcheck
		if _, err := refTx.Exec(ctx, `
			UPDATE squads
			SET leader_agent_id = $1
			WHERE id = $2
		`, globalAgent.ID, squad.ID); err != nil {
			t.Fatalf("stage global Squad leader: %v", err)
		}

		ownerTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer ownerTx.Rollback(ctx) //nolint:errcheck
		if _, err := ownerTx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
			t.Fatal(err)
		}
		_, err = ownerTx.Exec(ctx, `
			UPDATE agents
			SET project_id = $1
			WHERE id = $2
		`, projectB.ID, globalAgent.ID)
		assertSquadPostgresCode(t, err, "55P03")
		if err := ownerTx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		if err := refTx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `
			UPDATE agents
			SET project_id = $1
			WHERE id = $2
		`, projectB.ID, globalAgent.ID)
		assertSquadPostgresCode(t, err, "23514")
	})
}

func TestSquadStoreRechecksEnabledAgentAfterLockWait(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	project, agents := createSquadProjectWithAgents(t, s, "squad-lock-disabled", 2)
	if agents[1].State != "ENABLED" {
		t.Fatalf("fixture Agent state = %q, want ENABLED", agents[1].State)
	}

	disableTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer disableTx.Rollback(ctx) //nolint:errcheck
	if _, err := disableTx.Exec(ctx, `UPDATE agents SET state = 'DISABLED' WHERE id = $1`, agents[1].ID); err != nil {
		t.Fatalf("stage Agent disable: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := s.CreateSquad(ctx, store.Squad{
			ProjectID:     project.ID,
			Name:          "Disabled race",
			LeaderAgentID: agents[0].ID,
			Members:       []store.SquadMember{{Type: store.SquadMemberTypeAgent, ID: agents[1].ID}},
		})
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("CreateSquad returned before disable committed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := disableTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, store.ErrInvalidArgument) {
			t.Fatalf("CreateSquad error = %v, want ErrInvalidArgument", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CreateSquad did not finish after Agent disable committed")
	}
}

func assertSquadPostgresCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected PostgreSQL error %s", code)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("error = %v, want PostgreSQL code %s", err, code)
	}
}
