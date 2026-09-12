package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerIdempotencyKeyCollisionReturnsConflict(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	first := seedRunFixture(t, s, "idem-first")
	second := seedRunFixture(t, s, "idem-second")

	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID:      first.project.ID,
		RunID:          first.run.ID,
		Kind:           "START",
		IdempotencyKey: "shared-key",
	}); err != nil {
		t.Fatalf("enqueue first job: %v", err)
	}

	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID:      second.project.ID,
		RunID:          second.run.ID,
		Kind:           "START",
		IdempotencyKey: "shared-key",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-job collision error=%v, want ErrConflict", err)
	}

	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID:      first.project.ID,
		RunID:          first.run.ID,
		Kind:           "RESUME",
		IdempotencyKey: "shared-key",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("kind collision error=%v, want ErrConflict", err)
	}
}

func TestConfigurationOwnershipChangesCannotBreakProjectScope(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	first := seedRunFixture(t, s, "owner-first")
	second := seedRunFixture(t, s, "owner-second")

	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET project_id = $1 WHERE id = $2`, second.project.ID, first.model.ID); err == nil {
		t.Fatal("expected referenced model profile ownership change to fail")
	}

	orphanModel, err := s.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID:  &first.project.ID,
		ProviderID: first.provider.ID,
		Name:       "orphan-model",
		Model:      "test",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create orphan model profile: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET project_id = $1 WHERE id = $2`, second.project.ID, orphanModel.ID); err != nil {
		t.Fatalf("move unreferenced model profile: %v", err)
	}
}

func TestImmutableEvidenceBlocksParentDeletion(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "retention")

	if err := s.PutRunProvenance(ctx, f.project.ID, f.run.ID, []byte(`{"retained":true}`)); err != nil {
		t.Fatalf("put run provenance: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, f.run.ID); err == nil {
		t.Fatal("expected run deletion with provenance to be restricted")
	}

	historyAgent, err := s.CreateAgent(ctx, store.Agent{
		ProjectID:      &f.project.ID,
		Name:           "history-agent",
		Engine:         "test",
		ModelProfileID: f.model.ID,
		EngineSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("create history agent: %v", err)
	}
	if _, err := s.AppendEvent(ctx, store.Event{
		ProjectID: f.project.ID,
		AgentID:   &historyAgent.ID,
		Type:      "agent.history",
	}); err != nil {
		t.Fatalf("append retained event: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM agents WHERE id = $1`, historyAgent.ID); err == nil {
		t.Fatal("expected agent deletion with retained event to be restricted")
	}
}
