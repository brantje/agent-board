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
