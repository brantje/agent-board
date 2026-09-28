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
