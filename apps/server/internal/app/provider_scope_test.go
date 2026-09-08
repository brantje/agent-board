package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type providerVisibilityStore struct {
	fakeStore
	providers map[string]store.Provider
}

func (s *providerVisibilityStore) GetProvider(_ context.Context, scope *string, id string) (store.Provider, error) {
	p, ok := s.providers[id]
	if !ok {
		return store.Provider{}, store.ErrNotFound
	}
	if p.ProjectID == nil {
		return p, nil
	}
	if scope != nil && *scope == *p.ProjectID {
		return p, nil
	}
	return store.Provider{}, store.ErrNotFound
}

func (s *providerVisibilityStore) CreateModelProfile(_ context.Context, v store.ModelProfile) (store.ModelProfile, error) {
	return v, nil
}

func (s *providerVisibilityStore) UpdateModelProfile(_ context.Context, _ *string, v store.ModelProfile) (store.ModelProfile, error) {
	return v, nil
}

func TestModelProfileRejectsProviderOutsideScope(t *testing.T) {
	projectA := "project-a"
	projectB := "project-b"
	sharedID := "shared-provider"
	ownedAID := "project-a-provider"
	ownedBID := "project-b-provider"
	visibility := &providerVisibilityStore{
		fakeStore: fakeStore{project: store.Project{ID: projectA, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}},
		providers: map[string]store.Provider{
			sharedID: {ID: sharedID, Name: "Shared", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject},
			ownedAID: {ID: ownedAID, ProjectID: &projectA, Name: "A", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject},
			ownedBID: {ID: ownedBID, ProjectID: &projectB, Name: "B", Kind: "test", Enabled: true, SafeMetadata: store.EmptyObject},
		},
	}
	svc := New(visibility)
	ctx := context.Background()

	if _, err := svc.CreateModelProfile(ctx, modelProfileInput(&projectA, ownedBID)); !isProviderNotFound(err) {
		t.Fatalf("cross-project provider create err=%v", err)
	}
	if _, err := svc.CreateModelProfile(ctx, modelProfileInput(nil, ownedAID)); !isProviderNotFound(err) {
		t.Fatalf("shared profile with project provider err=%v", err)
	}
	if _, err := svc.CreateModelProfile(ctx, modelProfileInput(&projectA, sharedID)); err != nil {
		t.Fatalf("project profile with shared provider err=%v", err)
	}
	if _, err := svc.CreateModelProfile(ctx, modelProfileInput(&projectA, ownedAID)); err != nil {
		t.Fatalf("project profile with same-project provider err=%v", err)
	}

	if _, err := svc.UpdateModelProfile(ctx, &projectA, modelProfileInput(&projectA, ownedBID)); !isProviderNotFound(err) {
		t.Fatalf("cross-project provider update err=%v", err)
	}
	if _, err := svc.UpdateModelProfile(ctx, nil, modelProfileInput(nil, ownedAID)); !isProviderNotFound(err) {
		t.Fatalf("shared profile update with project provider err=%v", err)
	}
}

func modelProfileInput(projectID *string, providerID string) store.ModelProfile {
	return store.ModelProfile{
		ProjectID:          projectID,
		ProviderID:         providerID,
		Name:               "Model",
		Model:              "model",
		GenerationSettings: store.EmptyObject,
		Enabled:            true,
	}
}

func isProviderNotFound(err error) bool {
	appErr, ok := AsError(err)
	return ok && appErr.Code == "provider_not_found" && errors.Is(err, store.ErrNotFound)
}
