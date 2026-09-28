package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type sourceServiceTestStore struct {
	store.ControlPlaneStore
	projects    map[string]store.Project
	connections map[string]store.SourceConnection
	repositories map[string]store.SourceRepository
}

func (s *sourceServiceTestStore) GetProject(_ context.Context, id string) (store.Project, error) {
	value, ok := s.projects[id]
	if !ok {
		return store.Project{}, store.ErrNotFound
	}
	return value, nil
}

func (s *sourceServiceTestStore) ListSourceConnections(_ context.Context, scope *string) ([]store.SourceConnection, error) {
	out := make([]store.SourceConnection, 0)
	for _, value := range s.connections {
		if value.ProjectID == nil || (scope != nil && *value.ProjectID == *scope) {
			out = append(out, value)
		}
	}
	return out, nil
}

func (s *sourceServiceTestStore) GetSourceConnection(_ context.Context, scope *string, id string) (store.SourceConnection, error) {
	value, ok := s.connections[id]
	if !ok || (value.ProjectID != nil && (scope == nil || *value.ProjectID != *scope)) {
		return store.SourceConnection{}, store.ErrNotFound
	}
	return value, nil
}

func (s *sourceServiceTestStore) CreateSourceConnection(_ context.Context, value store.SourceConnection) (store.SourceConnection, error) {
	value.ID = "connection-1"
	s.connections[value.ID] = value
	return value, nil
}

func (s *sourceServiceTestStore) UpdateSourceConnection(_ context.Context, scope *string, value store.SourceConnection) (store.SourceConnection, error) {
	current, ok := s.connections[value.ID]
	if !ok || (current.ProjectID != nil && (scope == nil || *current.ProjectID != *scope)) {
		return store.SourceConnection{}, store.ErrNotFound
	}
	s.connections[value.ID] = value
	return value, nil
}

func (s *sourceServiceTestStore) ListSourceRepositories(_ context.Context, _ *string, connectionID string) ([]store.SourceRepository, error) {
	out := make([]store.SourceRepository, 0)
	for _, value := range s.repositories {
		if value.SourceConnectionID == connectionID {
			out = append(out, value)
		}
	}
	return out, nil
}

func (s *sourceServiceTestStore) GetSourceRepository(_ context.Context, _ *string, connectionID, repositoryID string) (store.SourceRepository, error) {
	value, ok := s.repositories[repositoryID]
	if !ok || value.SourceConnectionID != connectionID {
		return store.SourceRepository{}, store.ErrNotFound
	}
	return value, nil
}

func (s *sourceServiceTestStore) UpsertSourceRepository(_ context.Context, value store.SourceRepository) (store.SourceRepository, error) {
	if value.ID == "" {
		value.ID = "repository-1"
	}
	s.repositories[value.ID] = value
	return value, nil
}

func newSourceServiceTestStore() *sourceServiceTestStore {
	return &sourceServiceTestStore{
		projects: map[string]store.Project{"project-1": {ID: "project-1", Name: "Project"}},
		connections: make(map[string]store.SourceConnection),
		repositories: make(map[string]store.SourceRepository),
	}
}

func TestNormalizeSourceConnectionHostSemantics(t *testing.T) {
	github, err := normalizeSourceConnection(store.SourceConnection{Kind: " github ", Name: " GitHub ", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if github.BaseURL == nil || *github.BaseURL != "https://github.com" {
		t.Fatalf("GitHub base URL = %v", github.BaseURL)
	}

	gitlab, err := normalizeSourceConnection(store.SourceConnection{Kind: store.SourceProviderGitLab, Name: "GitLab", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if gitlab.BaseURL == nil || *gitlab.BaseURL != "https://gitlab.com" {
		t.Fatalf("GitLab base URL = %v", gitlab.BaseURL)
	}

	if _, err := normalizeSourceConnection(store.SourceConnection{Kind: store.SourceProviderForgejo, Name: "Forgejo", Enabled: true}); err == nil {
		t.Fatal("Forgejo without an instance URL was accepted")
	}
}

func TestNormalizeSourceConnectionRejectsUnsafeBaseURLShapes(t *testing.T) {
	for _, raw := range []string{
		"ftp://forgejo.example.test",
		"https://user@forgejo.example.test",
		"https://forgejo.example.test?x=1",
		"https://forgejo.example.test#fragment",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := normalizeSourceConnection(store.SourceConnection{
				Kind: store.SourceProviderForgejo, Name: "Forgejo", BaseURL: stringPointer(raw), Enabled: true,
			}); err == nil {
				t.Fatalf("base URL %q was accepted", raw)
			}
		})
	}
}

func TestSourceConnectionUpdatePreservesStoredReference(t *testing.T) {
	fake := newSourceServiceTestStore()
	ref := "source-connection:connection-1"
	fake.connections["connection-1"] = store.SourceConnection{
		ID: "connection-1", Kind: store.SourceProviderGitHub, Name: "Old", CredentialRef: &ref, Enabled: true,
	}
	service := New(fake)
	updated, err := service.UpdateSourceConnection(t.Context(), nil, store.SourceConnection{
		ID: "connection-1", Kind: store.SourceProviderGitHub, Name: "New", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CredentialRef == nil || *updated.CredentialRef != ref {
		t.Fatalf("stored reference was not preserved: %#v", updated.CredentialRef)
	}
}

func TestSourceRepositoryTrustedUpsertRequiresVisibleConnection(t *testing.T) {
	fake := newSourceServiceTestStore()
	service := New(fake)
	_, err := service.UpsertSourceRepository(t.Context(), nil, store.SourceRepository{
		SourceConnectionID: "missing", ExternalID: "42", Name: "repo", Path: "owner/repo", WebURL: "web",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %v, want not found", err)
	}
}


func TestPrepareConnectedProjectSourceUsesAuthoritativeRepositoryIdentity(t *testing.T) {
	fake := newSourceServiceTestStore()
	connection := store.SourceConnection{ID: "connection-1", Kind: store.SourceProviderGitHub, Name: "GitHub", Enabled: true}
	fake.connections[connection.ID] = connection
	fake.repositories["repository-1"] = store.SourceRepository{
		ID: "repository-1", SourceConnectionID: connection.ID, ExternalID: "42",
		Name: "widget", Path: "acme/widget", WebURL: "https://github.com/acme/widget", DefaultBranch: "main",
	}
	service := New(fake)
	connectionID, repositoryID := connection.ID, "repository-1"
	project, err := service.ensureProjectRepository(t.Context(), store.Project{
		Name: "Connected", IssuePrefix: "CON", SourceType: store.ProjectSourceConnected,
		SourceConnectionID: &connectionID, SourceRepositoryID: &repositoryID,
		RepositoryPath: "/caller/controlled", DefaultBranch: "caller-branch",
	})
	if err != nil {
		t.Fatal(err)
	}
	if project.SourceRef == nil || *project.SourceRef != "main" {
		t.Fatalf("source ref = %#v, want repository default branch", project.SourceRef)
	}
	if project.CloneURL != nil || project.RepositoryPath != "" || project.DefaultBranch != "" {
		t.Fatalf("connected source retained caller-controlled local/git fields: %+v", project)
	}
}

func TestPrepareConnectedProjectSourceRejectsCrossProjectConnection(t *testing.T) {
	fake := newSourceServiceTestStore()
	otherProject := "project-2"
	fake.connections["connection-2"] = store.SourceConnection{
		ID: "connection-2", ProjectID: &otherProject, Kind: store.SourceProviderForgejo, Name: "Other", Enabled: true,
	}
	fake.repositories["repository-2"] = store.SourceRepository{
		ID: "repository-2", SourceConnectionID: "connection-2", ExternalID: "9",
		Name: "repo", Path: "other/repo", WebURL: "https://forgejo.example/other/repo", DefaultBranch: "main",
	}
	service := New(fake)
	connectionID, repositoryID := "connection-2", "repository-2"
	_, err := service.ensureProjectRepository(t.Context(), store.Project{
		ID: "project-1", Name: "Project", IssuePrefix: "PRJ", SourceType: store.ProjectSourceConnected,
		SourceConnectionID: &connectionID, SourceRepositoryID: &repositoryID,
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project connected source error = %v, want not found", err)
	}
}


func TestProductionServicesPreserveSourceStoreCapability(t *testing.T) {
	fake := newSourceServiceTestStore()
	services, err := NewServicesWithRuntimes(fake, workspaceMaterializerFunc(func(_ context.Context, _ store.Project, _ store.Issue, workspace store.Workspace) (store.Workspace, error) {
		return workspace, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = services.Close() })
	if services.ControlPlane.sources != fake {
		t.Fatal("Source Store was not bound to the authoritative control-plane store")
	}
	if _, err := services.ControlPlane.CreateSourceConnection(t.Context(), store.SourceConnection{
		Kind: store.SourceProviderGitHub, Name: "Production GitHub", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
}


func TestSourceServiceCRUDAndRepositoryReadPaths(t *testing.T) {
	fake := newSourceServiceTestStore()
	service := New(fake)
	projectID := "project-1"

	created, err := service.CreateSourceConnection(t.Context(), store.SourceConnection{
		ProjectID: &projectID,
		Kind:      store.SourceProviderGitLab,
		Name:      " Project GitLab ",
		Enabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ProjectID == nil || *created.ProjectID != projectID || created.BaseURL == nil || *created.BaseURL != "https://gitlab.com" {
		t.Fatalf("created connection = %+v", created)
	}

	listed, err := service.ListSourceConnections(t.Context(), &projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed connections = %+v", listed)
	}
	got, err := service.GetSourceConnection(t.Context(), &projectID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Fatalf("got connection = %+v", got)
	}

	updated, err := service.UpdateSourceConnection(t.Context(), &projectID, store.SourceConnection{
		ID:      created.ID,
		Kind:    store.SourceProviderGitLab,
		Name:    "Renamed",
		Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Renamed" || updated.Enabled {
		t.Fatalf("updated connection = %+v", updated)
	}

	repository, err := service.UpsertSourceRepository(t.Context(), &projectID, store.SourceRepository{
		SourceConnectionID: created.ID,
		ExternalID:         "repo-42",
		Namespace:          "acme",
		Name:               "widget",
		Path:               "acme/widget",
		WebURL:             "https://gitlab.com/acme/widget",
		DefaultBranch:      "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	repositories, err := service.ListSourceRepositories(t.Context(), &projectID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0].ID != repository.ID {
		t.Fatalf("listed repositories = %+v", repositories)
	}
	gotRepository, err := service.GetSourceRepository(t.Context(), &projectID, created.ID, repository.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRepository.ExternalID != "repo-42" {
		t.Fatalf("repository = %+v", gotRepository)
	}
}

func TestSourceValidationAndUnavailableStoreBranches(t *testing.T) {
	service := New(&missingProjectStore{})
	if _, err := service.ListSourceConnections(t.Context(), nil); err == nil {
		t.Fatal("missing Source Store was accepted")
	}

	for name, input := range map[string]store.SourceConnection{
		"blank name":        {Kind: store.SourceProviderGitHub, Name: "   ", Enabled: true},
		"unknown kind":      {Kind: "bitbucket", Name: "Unknown", Enabled: true},
		"blank external id": {Kind: store.SourceProviderGitHub, Name: "GitHub", ExternalAccountID: stringPointer("  "), Enabled: true},
		"invalid health":    {Kind: store.SourceProviderGitHub, Name: "GitHub", HealthStatus: "BROKEN", Enabled: true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeSourceConnection(input); err == nil {
				t.Fatalf("%s accepted: %+v", name, input)
			}
		})
	}

	baseURL := "HTTPS://GitLab.Example.test/root///"
	normalized, err := normalizeSourceConnection(store.SourceConnection{
		Kind: store.SourceProviderGitLab, Name: "GitLab", BaseURL: &baseURL, ExternalAccountID: stringPointer(" account "), Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.BaseURL == nil || *normalized.BaseURL != "https://gitlab.example.test/root" ||
		normalized.ExternalAccountID == nil || *normalized.ExternalAccountID != "account" ||
		normalized.HealthStatus != "UNKNOWN" {
		t.Fatalf("normalized connection = %+v", normalized)
	}

	if err := validateSourceRepository(store.SourceRepository{}); err == nil {
		t.Fatal("empty Source Repository was accepted")
	}

	fake := newSourceServiceTestStore()
	connection := store.SourceConnection{ID: "connection-1", Kind: store.SourceProviderGitHub, Name: "GitHub", Enabled: true}
	fake.connections[connection.ID] = connection
	fake.repositories["repository-no-branch"] = store.SourceRepository{
		ID: "repository-no-branch", SourceConnectionID: connection.ID, ExternalID: "1",
		Name: "repo", Path: "acme/repo", WebURL: "https://github.com/acme/repo",
	}
	connected := New(fake)
	if _, err := connected.prepareConnectedProjectSource(t.Context(), store.Project{SourceType: store.ProjectSourceConnected}); err == nil {
		t.Fatal("connected source without durable identities was accepted")
	}
	connectionID, repositoryID := connection.ID, "repository-no-branch"
	if _, err := connected.prepareConnectedProjectSource(t.Context(), store.Project{
		SourceType: store.ProjectSourceConnected, SourceConnectionID: &connectionID, SourceRepositoryID: &repositoryID,
	}); err == nil {
		t.Fatal("connected source without target/default ref was accepted")
	}
}
