package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestGetWorkspaceForRuntimeIsProjectScoped(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "runtime-workspace-lookup")
	other := seedRunFixture(t, s, "runtime-workspace-lookup-other")

	workspace, err := s.GetWorkspace(ctx, fixture.project.ID, fixture.workspace.ID)
	if err != nil || workspace.ID != fixture.workspace.ID || workspace.IssueID != fixture.issue.ID {
		t.Fatalf("GetWorkspace() workspace=%+v err=%v", workspace, err)
	}
	if _, err := s.GetWorkspace(ctx, other.project.ID, fixture.workspace.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project GetWorkspace() error=%v", err)
	}
	if _, err := s.GetWorkspace(ctx, "", fixture.workspace.ID); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank project GetWorkspace() error=%v", err)
	}
	if _, err := s.UpdateWorkspaceCurrentBranch(ctx, fixture.project.ID, fixture.workspace.ID, " "); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank branch error=%v", err)
	}
	updated, err := s.UpdateWorkspaceCurrentBranch(ctx, fixture.project.ID, fixture.workspace.ID, "agent/ab-1")
	if err != nil || updated.CurrentBranch == nil || *updated.CurrentBranch != "agent/ab-1" {
		t.Fatalf("UpdateWorkspaceCurrentBranch()=%+v err=%v", updated, err)
	}
	if _, err := s.UpdateWorkspaceCurrentBranch(ctx, other.project.ID, fixture.workspace.ID, "agent/ab-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project UpdateWorkspaceCurrentBranch() error=%v", err)
	}
}

func TestWorkspaceCurrentRevisionPersistsAndIsProjectScoped(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "runtime-workspace-revision")
	other := seedRunFixture(t, s, "runtime-workspace-revision-other")

	const baseRevision = "1111111111111111111111111111111111111111"
	if _, err := s.MarkWorkspaceBootstrapReady(
		ctx,
		fixture.project.ID,
		fixture.issue.ID,
		fixture.workspace.ID,
		fixture.workspace.Path,
		fixture.project.RepositoryPath,
		fixture.project.DefaultBranch,
		baseRevision,
		fixture.workspace.WorkingBranch,
	); err != nil {
		t.Fatalf("MarkWorkspaceBootstrapReady() error=%v", err)
	}

	if got, err := s.GetWorkspaceCurrentRevision(ctx, fixture.project.ID, fixture.workspace.ID); err != nil || got != "" {
		t.Fatalf("initial GetWorkspaceCurrentRevision()=%q err=%v", got, err)
	}

	const revision = "0123456789abcdef0123456789abcdef01234567"
	if got, err := s.UpdateWorkspaceCurrentRevision(ctx, fixture.project.ID, fixture.workspace.ID, revision); err != nil || got != revision {
		t.Fatalf("UpdateWorkspaceCurrentRevision()=%q err=%v", got, err)
	}
	if got, err := s.GetWorkspaceCurrentRevision(ctx, fixture.project.ID, fixture.workspace.ID); err != nil || got != revision {
		t.Fatalf("persisted GetWorkspaceCurrentRevision()=%q err=%v", got, err)
	}

	if _, err := s.GetWorkspaceCurrentRevision(ctx, "", fixture.workspace.ID); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank project GetWorkspaceCurrentRevision() error=%v", err)
	}
	if _, err := s.UpdateWorkspaceCurrentRevision(ctx, fixture.project.ID, fixture.workspace.ID, " "); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank revision UpdateWorkspaceCurrentRevision() error=%v", err)
	}
	if _, err := s.GetWorkspaceCurrentRevision(ctx, other.project.ID, fixture.workspace.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project GetWorkspaceCurrentRevision() error=%v", err)
	}
	if _, err := s.UpdateWorkspaceCurrentRevision(ctx, other.project.ID, fixture.workspace.ID, revision); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project UpdateWorkspaceCurrentRevision() error=%v", err)
	}
}
