package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestUpdateProviderHealthPersistsCountsWithoutTouchingUpdatedAt(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	provider, err := s.CreateProvider(ctx, store.Provider{Name: "Health Provider", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	before := provider.UpdatedAt
	filtered, total := 2, 2
	if err := s.UpdateProviderHealth(ctx, provider.ID, "HEALTHY", &filtered, &total); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetProvider(ctx, nil, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HealthStatus != "HEALTHY" || loaded.FilteredModelCount == nil || *loaded.FilteredModelCount != 2 || loaded.TotalModelCount == nil || *loaded.TotalModelCount != 2 {
		t.Fatalf("loaded=%+v", loaded)
	}
	if !loaded.UpdatedAt.Equal(before) {
		t.Fatalf("updatedAt changed from %s to %s", before, loaded.UpdatedAt)
	}

	if err := s.UpdateProviderHealth(ctx, provider.ID, "UNHEALTHY", nil, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.GetProvider(ctx, nil, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HealthStatus != "UNHEALTHY" || loaded.FilteredModelCount == nil || *loaded.FilteredModelCount != 2 {
		t.Fatalf("counts should be preserved on unhealthy: %+v", loaded)
	}
}

func TestListAllProvidersReturnsSharedAndProjectOwned(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	project, err := s.CreateProject(ctx, testProjectInput("Health Scope", "/repos/health", "HL"))
	if err != nil {
		t.Fatal(err)
	}
	scope := &project.ID
	shared, err := s.CreateProvider(ctx, store.Provider{Name: "Shared", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	owned, err := s.CreateProvider(ctx, store.Provider{ProjectID: scope, Name: "Owned", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}

	all, err := s.ListAllProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := providerIDs(all)
	if !containsID(ids, shared.ID) || !containsID(ids, owned.ID) {
		t.Fatalf("all=%v", ids)
	}
}

func TestUpdateProviderHealthRejectsMissingProvider(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	count := 1
	err := s.UpdateProviderHealth(context.Background(), "00000000-0000-4000-8000-000000000001", "HEALTHY", &count, &count)
	if err == nil {
		t.Fatal("expected not found")
	}
}
