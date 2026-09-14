package postgres

import (
	"context"
	"errors"
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
