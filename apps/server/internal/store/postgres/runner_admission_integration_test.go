package postgres

import (
	"context"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"testing"
	"time"
)

func TestSchedulerRunnerPreferencePolicyAndCapacity(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "runner-placement")
	external, err := s.CreateRunner(ctx, store.Runner{Name: "External", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	internal, err := s.CreateRunner(ctx, store.Runner{Name: "Internal", Internal: true, TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	s.SetRunnerCandidates(func(engine string) []string {
		if engine != f.agent.Engine {
			t.Error("wrong engine")
		}
		return []string{internal.ID, external.ID}
	})
	enqueueFixtureRun(t, s, f, f.run, "runner-first")
	claim, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Second)
	if err != nil || claim == nil || claim.RunnerID != external.ID {
		t.Fatalf("external not preferred: %+v %v", claim, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE agents SET concurrency_limit=10 WHERE id=$1`, f.agent.ID); err != nil {
		t.Fatal(err)
	}
	second := createQueuedFixtureRun(t, s, f, "runner-second")
	enqueueFixtureRun(t, s, f, second, "runner-second")
	if _, err = s.pool.Exec(ctx, `UPDATE projects SET allow_internal_runner=false WHERE id=$1`, f.project.ID); err != nil {
		t.Fatal(err)
	}
	claim, err = s.AdmitNextJob(ctx, "worker", time.Minute, time.Millisecond)
	if err != nil || claim != nil {
		t.Fatal("disallowed fallback admitted")
	}
	if _, err = s.pool.Exec(ctx, `UPDATE projects SET allow_internal_runner=true WHERE id=$1;`, f.project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE scheduler_jobs SET available_at=now() WHERE run_id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	claim, err = s.AdmitNextJob(ctx, "worker", time.Minute, time.Millisecond)
	if err != nil || claim == nil || claim.RunnerID != internal.ID {
		t.Fatalf("internal fallback: %+v %v", claim, err)
	}
}

func TestSchedulerIgnoresDisconnectedAndMismatchedRunners(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "runner-stale")
	stale, err := s.CreateRunner(ctx, store.Runner{Name: "Stale", TokenHash: make([]byte, 32), Capabilities: []byte(`{"engines":["opencode"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ObserveRunner(ctx, stale.ID, []byte(`{"engines":["opencode"],"os":"linux"}`)); err != nil {
		t.Fatal(err)
	}
	s.SetRunnerCandidates(func(engine string) []string { return nil })
	enqueueFixtureRun(t, s, f, f.run, "stale-disconnected")
	claim, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Millisecond)
	if err != nil || claim != nil {
		t.Fatalf("disconnected runner admitted: %+v %v", claim, err)
	}

	wrongEngine, err := s.CreateRunner(ctx, store.Runner{Name: "Wrong engine", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	s.SetRunnerCandidates(func(engine string) []string {
		if engine == f.agent.Engine {
			return nil
		}
		return []string{wrongEngine.ID}
	})
	if _, err = s.pool.Exec(ctx, `UPDATE scheduler_jobs SET available_at=now() WHERE run_id=$1`, f.run.ID); err != nil {
		t.Fatal(err)
	}
	claim, err = s.AdmitNextJob(ctx, "worker", time.Minute, time.Millisecond)
	if err != nil || claim != nil {
		t.Fatalf("engine-mismatched runner admitted: %+v %v", claim, err)
	}
}
