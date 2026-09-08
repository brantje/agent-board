package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const projectBID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

type sharedVisibleProviderStore struct {
	fakeControlPlaneStore
}

func (s *sharedVisibleProviderStore) GetProvider(_ context.Context, _ *string, id string) (store.Provider, error) {
	if id != providerID {
		return store.Provider{}, store.ErrNotFound
	}
	secret := "secret-ref"
	return store.Provider{ID: providerID, Name: "Provider", Kind: "test", CredentialRef: &secret, Enabled: true, HealthStatus: "UNKNOWN", SafeMetadata: store.EmptyObject}, nil
}

func (s *sharedVisibleProviderStore) UpdateProvider(_ context.Context, scope *string, v store.Provider) (store.Provider, error) {
	if scope != nil {
		return store.Provider{}, store.ErrNotFound
	}
	return v, nil
}

type projectOwnedProviderStore struct {
	fakeControlPlaneStore
	owner string
}

func (s *projectOwnedProviderStore) GetProject(_ context.Context, id string) (store.Project, error) {
	if id != projectID && id != projectBID {
		return store.Project{}, store.ErrNotFound
	}
	return store.Project{ID: id, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, nil
}

func (s *projectOwnedProviderStore) GetProvider(_ context.Context, scope *string, id string) (store.Provider, error) {
	if id != providerID {
		return store.Provider{}, store.ErrNotFound
	}
	if scope == nil || *scope != s.owner {
		return store.Provider{}, store.ErrNotFound
	}
	return store.Provider{ID: providerID, ProjectID: &s.owner, Name: "Provider", Kind: "test", Enabled: true, HealthStatus: "UNKNOWN", SafeMetadata: store.EmptyObject}, nil
}

func (s *projectOwnedProviderStore) ListProviders(_ context.Context, scope *string) ([]store.Provider, error) {
	if scope == nil || *scope != s.owner {
		return nil, nil
	}
	owner := s.owner
	return []store.Provider{{ID: providerID, ProjectID: &owner, Name: "Provider", Kind: "test", Enabled: true, HealthStatus: "UNKNOWN", SafeMetadata: store.EmptyObject}}, nil
}

func TestProjectProviderSharedReadOnlyAndIsolation(t *testing.T) {
	body := `{"name":"Provider","kind":"test","safeMetadata":{}}`

	t.Run("shared provider is read-only in project scope", func(t *testing.T) {
		router := NewRouter(app.New(&sharedVisibleProviderStore{}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/projects/"+projectID+"/providers/"+providerID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "provider_not_found") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("other project cannot read project-owned provider", func(t *testing.T) {
		router := NewRouter(app.New(&projectOwnedProviderStore{owner: projectID}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectBID+"/providers/"+providerID, nil))
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "provider_not_found") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/providers/"+providerID, nil))
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "provider_not_found") {
			t.Fatalf("global get status=%d body=%s", rec.Code, rec.Body.String())
		}
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("global list status=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("project create stores credential with project secret scope", func(t *testing.T) {
		captured := &captureProviderCredentialStore{}
		writer := &fakeSecretWriter{}
		router := NewRouterWithSecrets(app.New(captured), writer)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/providers", strings.NewReader(`{"name":"Provider","kind":"openai-compatible","credential":"sk-project","safeMetadata":{}}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if writer.scope.ProjectID == nil || *writer.scope.ProjectID != projectID {
			t.Fatalf("secret scope project=%v", writer.scope.ProjectID)
		}
		if string(writer.value) != "sk-project" {
			t.Fatalf("stored credential=%q", writer.value)
		}
		var payload ProviderDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.ProjectID == nil || *payload.ProjectID != projectID {
			t.Fatalf("response projectId=%v", payload.ProjectID)
		}
		if strings.Contains(rec.Body.String(), "sk-project") {
			t.Fatal("credential leaked in response")
		}
	})
}
