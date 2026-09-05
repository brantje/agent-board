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
}
