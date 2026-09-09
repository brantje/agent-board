package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProviderSharedOrProjectOwnedIsolation(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	p1, err := s.CreateProject(ctx, testProjectInput("Provider Scope One", "/repos/provider-one", "PS1"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.CreateProject(ctx, testProjectInput("Provider Scope Two", "/repos/provider-two", "PS2"))
	if err != nil {
		t.Fatal(err)
	}
	scope1, scope2 := &p1.ID, &p2.ID

	shared, err := s.CreateProvider(ctx, store.Provider{Name: "Shared Provider", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	owned1, err := s.CreateProvider(ctx, store.Provider{ProjectID: scope1, Name: "Project Provider", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	owned2, err := s.CreateProvider(ctx, store.Provider{ProjectID: scope2, Name: "Project Provider", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.CreateProvider(ctx, store.Provider{ProjectID: scope1, Name: "Project Provider", Kind: "test", Enabled: true}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate project provider name err=%v", err)
	}
	if _, err = s.CreateProvider(ctx, store.Provider{Name: "Project Provider", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject}); err != nil {
		t.Fatalf("same name across shared and project should be allowed: %v", err)
	}

	global, err := s.ListProviders(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ids := providerIDs(global); !containsID(ids, shared.ID) || containsID(ids, owned1.ID) || containsID(ids, owned2.ID) {
		t.Fatalf("global list=%v", ids)
	}
	if _, err = s.GetProvider(ctx, nil, owned1.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("global get project-owned err=%v", err)
	}

	listed1, err := s.ListProviders(ctx, scope1)
	if err != nil {
		t.Fatal(err)
	}
	ids1 := providerIDs(listed1)
	if !containsID(ids1, shared.ID) || !containsID(ids1, owned1.ID) || containsID(ids1, owned2.ID) {
		t.Fatalf("project one list=%v", ids1)
	}
	if _, err = s.GetProvider(ctx, scope1, owned2.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project get err=%v", err)
	}
	if _, err = s.GetProvider(ctx, scope1, shared.ID); err != nil {
		t.Fatalf("shared visible in project err=%v", err)
	}

	owned1.Name = "Renamed Project Provider"
	if _, err = s.UpdateProvider(ctx, scope1, owned1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateProvider(ctx, scope1, shared); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("shared update in project scope err=%v", err)
	}
	if _, err = s.UpdateProvider(ctx, nil, owned1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("project-owned update via global err=%v", err)
	}

	if _, err = pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, p1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetProvider(ctx, scope2, owned1.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cascaded provider still visible err=%v", err)
	}
	if _, err = s.GetProvider(ctx, nil, shared.ID); err != nil {
		t.Fatalf("shared provider after project delete err=%v", err)
	}
}

func providerIDs(values []store.Provider) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.ID)
	}
	return out
}

func containsID(ids []string, id string) bool {
	for _, value := range ids {
		if value == id {
			return true
		}
	}
	return false
}
