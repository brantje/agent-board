package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectOwnedRunnerPersistenceRegistrationAndPolicyIsolation(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	ownerID := insertProject(t, s.pool, "owned-runner-owner")
	otherID := insertProject(t, s.pool, "owned-runner-other")

	registrationHash := make([]byte, 32)
	registrationHash[0] = 7
	pending, err := s.CreateRunner(ctx, store.Runner{ProjectID: &ownerID, RegistrationTokenHash: registrationHash})
	if err != nil {
		t.Fatal(err)
	}
	if pending.ProjectID == nil || *pending.ProjectID != ownerID {
		t.Fatalf("pending owner=%v", pending.ProjectID)
	}
	credentialHash := make([]byte, 32)
	credentialHash[0] = 8
	registered, err := s.RegisterRunner(ctx, registrationHash, store.Runner{Name: "Owned host", TokenHash: credentialHash})
	if err != nil {
		t.Fatal(err)
	}
	if registered.ProjectID == nil || *registered.ProjectID != ownerID {
		t.Fatalf("registration changed owner=%v", registered.ProjectID)
	}
	stored, err := s.GetRunner(ctx, registered.ID)
	if err != nil || stored.ProjectID == nil || *stored.ProjectID != ownerID {
		t.Fatalf("stored runner=%+v err=%v", stored, err)
	}
	shared, err := s.CreateRunner(ctx, store.Runner{Name: "Shared host", TokenHash: make([]byte, 32)})
	if err != nil || shared.ProjectID != nil {
		t.Fatalf("shared runner=%+v err=%v", shared, err)
	}
	if err := s.SetProjectRunnerIDs(ctx, otherID, []string{registered.ID}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("owned runner attached as shared capacity: %v", err)
	}
	if err := s.SetProjectRunnerIDs(ctx, ownerID, []string{registered.ID}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("owner redundantly attached owned runner: %v", err)
	}
	if err := s.SetProjectRunnerIDs(ctx, ownerID, []string{shared.ID}); err != nil {
		t.Fatalf("shared runner rejected: %v", err)
	}
}

func TestProjectOwnedRunnerOwnershipPersistsAcrossStoreReload(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	beforeReload := New(pool)
	owner := seedRunFixture(t, beforeReload, "owned-runner-reload-owner")
	foreign := seedRunFixture(t, beforeReload, "owned-runner-reload-foreign")

	owned, err := beforeReload.CreateRunner(ctx, store.Runner{
		ProjectID: &owner.project.ID,
		Name:      "Reload owned",
		TokenHash: make([]byte, 32),
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(ctx, os.Getenv(testDatabaseURLEnv))
	if err != nil {
		t.Fatalf("re-open store: %v", err)
	}
	defer reloaded.Close()

	stored, err := reloaded.GetRunner(ctx, owned.ID)
	if err != nil {
		t.Fatalf("load runner after store reload: %v", err)
	}
	if stored.ProjectID == nil || *stored.ProjectID != owner.project.ID {
		t.Fatalf("reloaded owner=%v want %s", stored.ProjectID, owner.project.ID)
	}

	reloaded.SetRunnerCandidates(func(string) []string { return []string{owned.ID} })
	enqueueFixtureRun(t, reloaded, foreign, foreign.run, "owned-runner-reload-foreign")
	claim, err := reloaded.AdmitNextJob(ctx, "worker", time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("admit foreign run after reload: %v", err)
	}
	if claim != nil {
		t.Fatalf("foreign project admitted reloaded owned runner: %+v", claim)
	}
}

func TestProjectOwnedRunnerSchedulerIsolation(t *testing.T) {
	t.Run("owner project", func(t *testing.T) {
		s := New(testPool(t))
		ctx := context.Background()
		f := seedRunFixture(t, s, "owned-runner-admission-owner")
		owned, err := s.CreateRunner(ctx, store.Runner{ProjectID: &f.project.ID, Name: "Owned", TokenHash: make([]byte, 32)})
		if err != nil {
			t.Fatal(err)
		}
		s.SetRunnerCandidates(func(string) []string { return []string{owned.ID} })
		enqueueFixtureRun(t, s, f, f.run, "owned-runner-owner")
		claim, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Millisecond)
		if err != nil || claim == nil || claim.RunnerID != owned.ID {
			t.Fatalf("owner admission=%+v err=%v", claim, err)
		}
	})

	t.Run("foreign project", func(t *testing.T) {
		s := New(testPool(t))
		ctx := context.Background()
		f := seedRunFixture(t, s, "owned-runner-admission-foreign")
		ownerID := insertProject(t, s.pool, "foreign-runner-owner")
		owned, err := s.CreateRunner(ctx, store.Runner{ProjectID: &ownerID, Name: "Foreign owned", TokenHash: make([]byte, 32)})
		if err != nil {
			t.Fatal(err)
		}
		s.SetRunnerCandidates(func(string) []string { return []string{owned.ID} })
		enqueueFixtureRun(t, s, f, f.run, "owned-runner-foreign")
		claim, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Millisecond)
		if err != nil || claim != nil {
			t.Fatalf("foreign owned runner admitted=%+v err=%v", claim, err)
		}
	})
}

func TestExecutionSessionRejectsForeignProjectOwnedRunner(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "owned-runner-session")
	ownerID := insertProject(t, s.pool, "session-runner-owner")
	owned, err := s.CreateRunner(ctx, store.Runner{ProjectID: &ownerID, Name: "Foreign session runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateExecutionSession(ctx, store.ExecutionSession{ProjectID: f.project.ID, RunID: f.run.ID, RunnerID: owned.ID, Status: "PENDING"})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("foreign runner session error=%v, want ErrInvalidArgument", err)
	}
}

func TestProjectOwnedRunnerDeletionPreservesExecutionProvenance(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "owned-runner-provenance")
	owned, err := s.CreateRunner(ctx, store.Runner{
		ProjectID: &f.project.ID,
		Name:      "Historical owned",
		TokenHash: make([]byte, 32),
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID:     f.run.ID,
		RunnerID:  owned.ID,
		Status:    "PENDING",
	})
	if err != nil {
		t.Fatalf("create execution session: %v", err)
	}
	if _, err := s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID:    f.project.ID,
		SessionID:    session.ID,
		FromStatuses: []string{"PENDING"},
		Status:       "COMPLETED",
	}); err != nil {
		t.Fatalf("complete execution session: %v", err)
	}

	deleted, err := s.RevokeRunner(ctx, owned.ID, true)
	if err != nil {
		t.Fatalf("delete owned runner: %v", err)
	}
	if deleted.RevokedAt == nil || deleted.DeletedAt == nil {
		t.Fatalf("deleted runner lifecycle state=%+v", deleted)
	}

	historical, err := s.GetExecutionSession(ctx, f.project.ID, session.ID)
	if err != nil {
		t.Fatalf("reload historical execution session: %v", err)
	}
	if historical.RunnerID != owned.ID {
		t.Fatalf("historical runner_id=%s want %s", historical.RunnerID, owned.ID)
	}
	sessions, err := s.ListExecutionSessionsByRunner(ctx, owned.ID, []string{"COMPLETED"})
	if err != nil || len(sessions) != 1 || sessions[0].ID != session.ID || sessions[0].RunnerID != owned.ID {
		t.Fatalf("historical runner sessions=%+v err=%v", sessions, err)
	}
	if _, err := s.GetRunner(ctx, owned.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted runner remained lifecycle-visible: %v", err)
	}

	var persistedID, persistedProjectID string
	var revokedAt, deletedAt *time.Time
	if err := s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, revoked_at, deleted_at
		FROM runners
		WHERE id=$1
	`, owned.ID).Scan(&persistedID, &persistedProjectID, &revokedAt, &deletedAt); err != nil {
		t.Fatalf("load soft-deleted runner row: %v", err)
	}
	if persistedID != owned.ID || persistedProjectID != f.project.ID || revokedAt == nil || deletedAt == nil {
		t.Fatalf("persisted deleted runner id=%s project=%s revoked=%v deleted=%v", persistedID, persistedProjectID, revokedAt, deletedAt)
	}

	s.SetRunnerCandidates(func(string) []string { return []string{owned.ID} })
	newRun := createQueuedFixtureRun(t, s, f, "owned-runner-provenance-new-work")
	enqueueFixtureRun(t, s, f, newRun, "owned-runner-provenance-new-work")
	claim, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("admit with deleted runner candidate: %v", err)
	}
	if claim != nil {
		t.Fatalf("deleted runner admitted for new work: %+v", claim)
	}
}
