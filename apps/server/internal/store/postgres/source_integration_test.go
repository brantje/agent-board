package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSourceStoreScopeAndRepositoryRefresh(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	a, err := s.CreateProject(ctx, testProjectInput("source-a", "/repo/source-a", "SRA"))
	if err != nil { t.Fatal(err) }
	b, err := s.CreateProject(ctx, testProjectInput("source-b", "/repo/source-b", "SRB"))
	if err != nil { t.Fatal(err) }
	global, err := s.CreateSourceConnection(ctx, store.SourceConnection{Kind: store.SourceProviderGitHub, Name: "Shared", Enabled: true})
	if err != nil { t.Fatal(err) }
	scoped, err := s.CreateSourceConnection(ctx, store.SourceConnection{ProjectID: &a.ID, Kind: store.SourceProviderForgejo, Name: "Scoped", Enabled: true})
	if err != nil { t.Fatal(err) }
	got, err := s.ListSourceConnections(ctx, &b.ID)
	if err != nil { t.Fatal(err) }
	if len(got) != 1 || got[0].ID != global.ID { t.Fatalf("visible connections = %#v", got) }
	if _, err := s.GetSourceConnection(ctx, &b.ID, scoped.ID); !errors.Is(err, store.ErrNotFound) { t.Fatalf("cross-project lookup = %v", err) }
	first, err := s.UpsertSourceRepository(ctx, store.SourceRepository{SourceConnectionID: scoped.ID, ExternalID: "42", Namespace: "acme", Name: "widget", Path: "acme/widget", WebURL: "web", DefaultBranch: "main"})
	if err != nil { t.Fatal(err) }
	second, err := s.UpsertSourceRepository(ctx, store.SourceRepository{SourceConnectionID: scoped.ID, ExternalID: "42", Namespace: "platform", Name: "widget-new", Path: "platform/widget-new", WebURL: "web-new", DefaultBranch: "trunk", Archived: true})
	if err != nil { t.Fatal(err) }
	if second.ID != first.ID || second.Path != "platform/widget-new" || second.DefaultBranch != "trunk" || !second.Archived { t.Fatalf("refresh = %#v", second) }
	if _, err := s.GetSourceRepository(ctx, &b.ID, scoped.ID, first.ID); !errors.Is(err, store.ErrNotFound) { t.Fatalf("cross-project repository lookup = %v", err) }
}


func TestConnectedProjectSourceEnforcesConnectionRepositoryIdentityAndPreservesDisabledBinding(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	project, err := s.CreateProject(ctx, testProjectInput("connected-source", "/repo/connected-source", "CSP"))
	if err != nil {
		t.Fatal(err)
	}
	connection, err := s.CreateSourceConnection(ctx, store.SourceConnection{
		ProjectID: &project.ID, Kind: store.SourceProviderGitLab, Name: "Project GitLab",
		BaseURL: stringPointer("https://gitlab.example.test"), Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := s.UpsertSourceRepository(ctx, store.SourceRepository{
		SourceConnectionID: connection.ID, ExternalID: "101", Namespace: "acme",
		Name: "widget", Path: "acme/widget", WebURL: "https://gitlab.example.test/acme/widget",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	project.SourceType = store.ProjectSourceConnected
	project.RepositoryPath = ""
	project.DefaultBranch = ""
	project.SourceConnectionID = &connection.ID
	project.SourceRepositoryID = &repository.ID
	project.SourceRef = stringPointer("main")
	connected, err := s.UpdateProject(ctx, project)
	if err != nil {
		t.Fatal(err)
	}

	other, err := s.CreateSourceConnection(ctx, store.SourceConnection{
		ProjectID: &project.ID, Kind: store.SourceProviderForgejo, Name: "Project Forgejo",
		BaseURL: stringPointer("https://forgejo.example.test"), Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	forged := connected
	forged.SourceConnectionID = &other.ID
	if _, err := s.UpdateProject(ctx, forged); err == nil {
		t.Fatal("cross-connection repository binding unexpectedly succeeded")
	}

	connection.Enabled = false
	disabled, err := s.UpdateSourceConnection(ctx, &project.ID, connection)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled {
		t.Fatal("source connection was not disabled")
	}
	persisted, err := s.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.SourceConnectionID == nil || *persisted.SourceConnectionID != connection.ID ||
		persisted.SourceRepositoryID == nil || *persisted.SourceRepositoryID != repository.ID {
		t.Fatalf("disabling connection rewrote Project source identity: %+v", persisted)
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM source_connections WHERE id=$1`, connection.ID); err == nil {
		t.Fatal("deleting a Source Connection referenced by a connected Project unexpectedly succeeded")
	}
}


func TestSourceStoreReadAndUpdatePaths(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	connection, err := s.CreateSourceConnection(ctx, store.SourceConnection{
		Kind: store.SourceProviderGitHub, Name: "Global read paths", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSourceConnection(ctx, nil, connection.ID)
	if err != nil || got.ID != connection.ID {
		t.Fatalf("GetSourceConnection() = %+v, %v", got, err)
	}
	connection.Name = "Global renamed"
	connection.HealthStatus = "HEALTHY"
	updated, err := s.UpdateSourceConnection(ctx, nil, connection)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Global renamed" || updated.HealthStatus != "HEALTHY" {
		t.Fatalf("updated connection = %+v", updated)
	}
	repository, err := s.UpsertSourceRepository(ctx, store.SourceRepository{
		SourceConnectionID: connection.ID, ExternalID: "read-1", Namespace: "acme",
		Name: "repo", Path: "acme/repo", WebURL: "https://github.com/acme/repo", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListSourceRepositories(ctx, nil, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != repository.ID {
		t.Fatalf("repositories = %+v", listed)
	}
	read, err := s.GetSourceRepository(ctx, nil, connection.ID, repository.ID)
	if err != nil || read.ExternalID != "read-1" {
		t.Fatalf("GetSourceRepository() = %+v, %v", read, err)
	}
}
