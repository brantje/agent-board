package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type providerModelsStore struct {
	fakeControlPlaneStore
	provider store.Provider
}

func (s *providerModelsStore) GetProvider(_ context.Context, _ *string, id string) (store.Provider, error) {
	if id != s.provider.ID {
		return store.Provider{}, store.ErrNotFound
	}
	return s.provider, nil
}

func (s *providerModelsStore) UpdateProviderHealth(context.Context, string, string, *int, *int) error {
	return nil
}

type providerModelsSecretResolver struct {
	values map[string][]byte
}

func (r *providerModelsSecretResolver) Resolve(_ context.Context, _ secrets.Scope, ref string) ([]byte, error) {
	value, ok := r.values[ref]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	return value, nil
}

func providerModelsRouter(t *testing.T, store *providerModelsStore, resolver executioncontext.SecretResolver) http.Handler {
	t.Helper()
	return newRouterWithReviews(app.New(store), nil, nil, resolver, nil, nil, nil, nil)
}

func TestListProviderModelsReturnsDiscoveredModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-a"}, {"id": "model-a"}, {"id": ""}},
		})
	}))
	defer upstream.Close()

	store := &providerModelsStore{provider: store.Provider{
		ID:           providerID,
		Kind:         "openai-compatible",
		BaseURL:      &[]string{upstream.URL}[0],
		Enabled:      true,
		SafeMetadata: store.EmptyObject,
	}}
	w := httptest.NewRecorder()
	providerModelsRouter(t, store, &providerModelsSecretResolver{}).
		ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/providers/"+providerID+"/models", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Models) != 1 || body.Models[0].ID != "model-a" || body.Total != 3 {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestListProviderModelsReturnsNotFoundForMissingProvider(t *testing.T) {
	store := &providerModelsStore{provider: store.Provider{ID: providerID, Kind: "openai", Enabled: true, SafeMetadata: store.EmptyObject}}
	w := httptest.NewRecorder()
	providerModelsRouter(t, store, &providerModelsSecretResolver{}).
		ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/providers/11111111-1111-4111-8111-111111111111/models", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestListProviderModelsReturnsUnconfiguredForCustomProviderWithoutBaseURL(t *testing.T) {
	store := &providerModelsStore{provider: store.Provider{
		ID:           providerID,
		Kind:         "llamarack",
		Enabled:      true,
		SafeMetadata: store.EmptyObject,
	}}
	w := httptest.NewRecorder()
	providerModelsRouter(t, store, &providerModelsSecretResolver{}).
		ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/providers/"+providerID+"/models", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestListProviderModelsReturnsCredentialUnavailable(t *testing.T) {
	ref := "provider:" + providerID
	store := &providerModelsStore{provider: store.Provider{
		ID:            providerID,
		Kind:          "openai",
		CredentialRef: &ref,
		Enabled:       true,
		SafeMetadata:  store.EmptyObject,
	}}
	w := httptest.NewRecorder()
	var resolver executioncontext.SecretResolver
	providerModelsRouter(t, store, resolver).
		ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/providers/"+providerID+"/models", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}
