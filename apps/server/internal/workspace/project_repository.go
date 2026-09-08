package workspace

import "context"

// ProjectRepositoryProvisioner ensures a configured Project repository path exists
// and is initialized as a Git repository before Workspace bootstrap.
type ProjectRepositoryProvisioner interface {
	EnsureProjectRepository(context.Context, string, string) (string, error)
}
