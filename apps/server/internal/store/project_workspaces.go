package store

// ProjectWorkspace is the single backend-owned writable Git checkout holding
// the latest accepted state for a Project. ProjectID is its durable identity;
// the remaining metadata is persisted by the Git checkout itself.
type ProjectWorkspace struct {
	ProjectID        string
	Path             string
	RepositoryPath   string
	BaseBranch       string
	AcceptedRevision string
}
