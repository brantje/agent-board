package workspace

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestWorkspaceRepositoryUsesProjectConfigurationUntilReady(t *testing.T) {
	project := store.Project{RepositoryPath: "/repositories", DefaultBranch: "main"}
	pending := store.Workspace{
		BootstrapStatus: "PENDING",
		RepositoryPath:  workspaceStringPointer("test"),
		BaseBranch:      workspaceStringPointer("legacy"),
	}

	repositoryPath, baseBranch := workspaceRepository(pending, project)
	if repositoryPath != "/repositories" || baseBranch != "main" {
		t.Fatalf("workspaceRepository() = (%q, %q), want project configuration while pending", repositoryPath, baseBranch)
	}
}

func TestWorkspaceRepositoryPinsReadyWorkspaceSource(t *testing.T) {
	project := store.Project{RepositoryPath: "/repositories", DefaultBranch: "main"}
	ready := store.Workspace{
		BootstrapStatus: "READY",
		RepositoryPath:  workspaceStringPointer("/repositories/pinned"),
		BaseBranch:      workspaceStringPointer("release"),
	}

	repositoryPath, baseBranch := workspaceRepository(ready, project)
	if repositoryPath != "/repositories/pinned" || baseBranch != "release" {
		t.Fatalf("workspaceRepository() = (%q, %q), want pinned ready workspace metadata", repositoryPath, baseBranch)
	}
}
