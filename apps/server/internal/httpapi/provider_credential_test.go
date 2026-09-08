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

type captureProviderCredentialStore struct {
	fakeControlPlaneStore
	created store.Provider
	updated store.Provider
}

func (s *captureProviderCredentialStore) CreateProvider(_ context.Context, p store.Provider) (store.Provider, error) {
	p.ID = providerID
	p.HealthStatus = "UNKNOWN"
	if p.SafeMetadata == nil {
		p.SafeMetadata = store.EmptyObject
	}
	s.created = p
	return p, nil
}

func (s *captureProviderCredentialStore) UpdateProvider(_ context.Context, p store.Provider) (store.Provider, error) {
	s.updated = p
	return p, nil
}

func providerRouterWithSecrets(t *testing.T, store *captureProviderCredentialStore, writer app.SecretWriter) http.Handler {
	t.Helper()
	return NewRouterWithSecrets(app.New(store), writer)
}

func TestProviderCreateStoresInlineCredentialWithoutLeakingPlaintext(t *testing.T) {
	store := &captureProviderCredentialStore{}
	writer := &fakeSecretWriter{}
	router := providerRouterWithSecrets(t, store, writer)

	body := `{"name":"Provider","kind":"openai-compatible","credential":"sk-test-key","safeMetadata":{}}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(body)))

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if string(writer.value) != "sk-test-key" {
		t.Fatalf("stored credential = %q", writer.value)
	}
	expectedRef := "provider:" + providerID
	if writer.ref != expectedRef {
		t.Fatalf("secret ref = %q, want %q", writer.ref, expectedRef)
	}
	if store.updated.CredentialRef == nil || *store.updated.CredentialRef != expectedRef {
		t.Fatalf("provider credential ref = %v", store.updated.CredentialRef)
	}
	if strings.Contains(w.Body.String(), "sk-test-key") || strings.Contains(w.Body.String(), expectedRef) {
		t.Fatal("credential material leaked in response")
	}
}

func TestProviderUpdateStoresInlineCredentialUsingExistingReference(t *testing.T) {
	store := &captureProviderCredentialStore{}
	writer := &fakeSecretWriter{}
	router := providerRouterWithSecrets(t, store, writer)

	body := `{"name":"Provider","kind":"openai-compatible","credential":"sk-rotated","safeMetadata":{}}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/providers/"+providerID, strings.NewReader(body)))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if string(writer.value) != "sk-rotated" {
		t.Fatalf("stored credential = %q", writer.value)
	}
	if writer.ref != "secret-ref" {
		t.Fatalf("secret ref = %q, want secret-ref", writer.ref)
	}
	if strings.Contains(w.Body.String(), "sk-rotated") || strings.Contains(w.Body.String(), "secret-ref") {
		t.Fatal("credential material leaked in response")
	}
}

func TestProviderInlineCredentialRequiresConfiguredSecretStorage(t *testing.T) {
	store := &captureProviderCredentialStore{}
	router := NewRouter(app.New(store))

	body := `{"name":"Provider","kind":"openai-compatible","credential":"sk-test-key","safeMetadata":{}}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(body)))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var payload ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "secret_storage_unavailable" {
		t.Fatalf("code = %q", payload.Error.Code)
	}
}
