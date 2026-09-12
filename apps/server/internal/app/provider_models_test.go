package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const testProviderID = "22222222-2222-4222-8222-222222222222"

type providerModelStore struct {
	fakeStore
	provider store.Provider
}

func (s *providerModelStore) GetProvider(_ context.Context, _ *string, id string) (store.Provider, error) {
	if id != s.provider.ID {
		return store.Provider{}, store.ErrNotFound
	}
	return s.provider, nil
}

func (s *providerModelStore) UpdateProviderHealth(context.Context, string, string, *int, *int) error {
	return nil
}

type fakeProviderModelSecretResolver struct {
	values map[string][]byte
	scope  secrets.Scope
}

func (f *fakeProviderModelSecretResolver) Resolve(_ context.Context, scope secrets.Scope, ref string) ([]byte, error) {
	f.scope = scope
	value, ok := f.values[ref]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	return value, nil
}

func TestListProviderModelsReturnsDiscoveredModels(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-a"}, {"id": "model-b"}},
		})
	}))
	defer upstream.Close()

	ref := "provider:" + testProviderID
	store := &providerModelStore{provider: store.Provider{
		ID:            testProviderID,
		Name:          "Provider",
		Kind:          "openai-compatible",
		BaseURL:       &[]string{upstream.URL}[0],
		CredentialRef: &ref,
		Enabled:       true,
		SafeMetadata:  store.EmptyObject,
	}}
	service := New(store)
	resolver := &fakeProviderModelSecretResolver{values: map[string][]byte{ref: []byte("secret")}}

	result, err := service.ListProviderModels(context.Background(), nil, testProviderID, resolver, upstream.Client())
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(result.Models) != 2 || result.Models[0].ID != "model-a" || result.Models[1].ID != "model-b" || result.Total != 2 {
		t.Fatalf("result=%+v", result)
	}
}

func TestListProviderModelsPersistsDistinctFilteredAndTotalCounts(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "model-a"},
				{"id": "model-a"},
				{"id": ""},
				{"id": "model-b"},
			},
		})
	}))
	defer upstream.Close()

	ref := "provider:" + testProviderID
	store := &providerHealthStore{provider: store.Provider{
		ID:            testProviderID,
		Kind:          "openai-compatible",
		BaseURL:       &[]string{upstream.URL}[0],
		CredentialRef: &ref,
		Enabled:       true,
		SafeMetadata:  store.EmptyObject,
	}}
	service := New(store)
	resolver := &fakeProviderModelSecretResolver{values: map[string][]byte{ref: []byte("secret")}}

	result, err := service.ListProviderModels(context.Background(), nil, testProviderID, resolver, upstream.Client())
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(result.Models) != 2 || result.Total != 4 {
		t.Fatalf("result=%+v", result)
	}
	update := store.lastHealthUpdate()
	if update.health != "HEALTHY" || update.filtered == nil || *update.filtered != 2 || update.total == nil || *update.total != 4 {
		t.Fatalf("update=%+v", update)
	}
}

func TestListProviderModelsRequiresSecretResolverWhenCredentialConfigured(t *testing.T) {
	ref := "provider:" + testProviderID
	store := &providerModelStore{provider: store.Provider{
		ID:            testProviderID,
		Kind:          "openai",
		CredentialRef: &ref,
		Enabled:       true,
		SafeMetadata:  store.EmptyObject,
	}}
	service := New(store)
	_, err := service.ListProviderModels(context.Background(), nil, testProviderID, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "provider_credential_unavailable" {
		t.Fatalf("err=%v", err)
	}
}

func TestListProviderModelsResolvesSharedProviderCredentialInOwnerScope(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-a"}},
		})
	}))
	defer upstream.Close()

	viewingProject := "project-1"
	ref := "provider:" + testProviderID
	store := &providerModelStore{
		fakeStore: fakeStore{project: store.Project{ID: viewingProject, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}},
		provider: store.Provider{
			ID:            testProviderID,
			Name:          "Shared",
			Kind:          "openai-compatible",
			BaseURL:       &[]string{upstream.URL}[0],
			CredentialRef: &ref,
			Enabled:       true,
			SafeMetadata:  store.EmptyObject,
		}}
	service := New(store)
	resolver := &fakeProviderModelSecretResolver{values: map[string][]byte{ref: []byte("shared-secret")}}

	if _, err := service.ListProviderModels(context.Background(), &viewingProject, testProviderID, resolver, upstream.Client()); err != nil {
		t.Fatalf("err=%v", err)
	}
	if resolver.scope.ProjectID != nil {
		t.Fatalf("secret scope project=%v, want provider owner (global)", *resolver.scope.ProjectID)
	}
}

func TestListProviderModelsResolvesOwnedProviderCredentialInOwnerScope(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-a"}},
		})
	}))
	defer upstream.Close()

	owner := "project-1"
	ref := "provider:" + testProviderID
	store := &providerModelStore{
		fakeStore: fakeStore{project: store.Project{ID: owner, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}},
		provider: store.Provider{
			ID:            testProviderID,
			ProjectID:     &owner,
			Name:          "Owned",
			Kind:          "openai-compatible",
			BaseURL:       &[]string{upstream.URL}[0],
			CredentialRef: &ref,
			Enabled:       true,
			SafeMetadata:  store.EmptyObject,
		}}
	service := New(store)
	resolver := &fakeProviderModelSecretResolver{values: map[string][]byte{ref: []byte("owned-secret")}}

	if _, err := service.ListProviderModels(context.Background(), &owner, testProviderID, resolver, upstream.Client()); err != nil {
		t.Fatalf("err=%v", err)
	}
	if resolver.scope.ProjectID == nil || *resolver.scope.ProjectID != owner {
		t.Fatalf("secret scope project=%v, want %s", resolver.scope.ProjectID, owner)
	}
}

func TestListProviderModelsRejectsMissingCredentialValue(t *testing.T) {
	ref := "provider:" + testProviderID
	store := &providerModelStore{provider: store.Provider{
		ID:            testProviderID,
		Kind:          "openai",
		CredentialRef: &ref,
		Enabled:       true,
		SafeMetadata:  store.EmptyObject,
	}}
	service := New(store)
	_, err := service.ListProviderModels(context.Background(), nil, testProviderID, &fakeProviderModelSecretResolver{values: map[string][]byte{}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "provider_credential_unavailable" {
		t.Fatalf("err=%v", err)
	}
}

func TestListProviderModelsRejectsUpstreamDiscoveryFailure(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	ref := "provider:" + testProviderID
	store := &providerModelStore{provider: store.Provider{
		ID:            testProviderID,
		Kind:          "openai-compatible",
		BaseURL:       &[]string{upstream.URL}[0],
		CredentialRef: &ref,
		Enabled:       true,
		SafeMetadata:  store.EmptyObject,
	}}
	service := New(store)
	resolver := &fakeProviderModelSecretResolver{values: map[string][]byte{ref: []byte("secret")}}
	_, err := service.ListProviderModels(context.Background(), nil, testProviderID, resolver, upstream.Client())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "provider_model_discovery_failed" {
		t.Fatalf("err=%v", err)
	}
}

func TestListProviderModelsRejectsHTTPCredentialedDiscovery(t *testing.T) {
	ref := "provider:" + testProviderID
	base := "http://example.com/v1"
	store := &providerModelStore{provider: store.Provider{
		ID:            testProviderID,
		Kind:          "custom",
		BaseURL:       &base,
		CredentialRef: &ref,
		Enabled:       true,
		SafeMetadata:  store.EmptyObject,
	}}
	service := New(store)
	resolver := &fakeProviderModelSecretResolver{values: map[string][]byte{ref: []byte("secret")}}
	_, err := service.ListProviderModels(context.Background(), nil, testProviderID, resolver, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "provider_model_discovery_failed" {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(apiErr.Message, "Unable to discover models") {
		t.Fatalf("message=%q", apiErr.Message)
	}
}

func TestListProviderModelsRejectsUnconfiguredCustomProvider(t *testing.T) {
	store := &providerModelStore{provider: store.Provider{
		ID:           testProviderID,
		Kind:         "llamarack",
		Enabled:      true,
		SafeMetadata: store.EmptyObject,
	}}
	service := New(store)
	_, err := service.ListProviderModels(context.Background(), nil, testProviderID, &fakeProviderModelSecretResolver{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "provider_model_discovery_unconfigured" {
		t.Fatalf("err=%v", err)
	}
}

var _ executioncontext.SecretResolver = (*fakeProviderModelSecretResolver)(nil)
