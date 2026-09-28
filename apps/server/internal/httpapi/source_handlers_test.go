package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const sourceConnectionTestID = "11111111-1111-4111-8111-111111111111"

type sourceHTTPStore struct {
	fakeControlPlaneStore
	connection   store.SourceConnection
	repositories []store.SourceRepository
}

func (s *sourceHTTPStore) ListSourceConnections(context.Context, *string) ([]store.SourceConnection, error) {
	if s.connection.ID == "" {
		return nil, nil
	}
	return []store.SourceConnection{s.connection}, nil
}

func (s *sourceHTTPStore) GetSourceConnection(_ context.Context, _ *string, id string) (store.SourceConnection, error) {
	if s.connection.ID != id {
		return store.SourceConnection{}, store.ErrNotFound
	}
	return s.connection, nil
}

func (s *sourceHTTPStore) CreateSourceConnection(_ context.Context, value store.SourceConnection) (store.SourceConnection, error) {
	value.ID = sourceConnectionTestID
	value.HealthStatus = "UNKNOWN"
	s.connection = value
	return value, nil
}

func (s *sourceHTTPStore) UpdateSourceConnection(_ context.Context, _ *string, value store.SourceConnection) (store.SourceConnection, error) {
	s.connection = value
	return value, nil
}

func (s *sourceHTTPStore) ListSourceRepositories(context.Context, *string, string) ([]store.SourceRepository, error) {
	return s.repositories, nil
}

func (s *sourceHTTPStore) GetSourceRepository(_ context.Context, _ *string, _, repositoryID string) (store.SourceRepository, error) {
	for _, value := range s.repositories {
		if value.ID == repositoryID {
			return value, nil
		}
	}
	return store.SourceRepository{}, store.ErrNotFound
}

func (s *sourceHTTPStore) UpsertSourceRepository(_ context.Context, value store.SourceRepository) (store.SourceRepository, error) {
	return value, nil
}

func TestSourceConnectionCreateStoresSecretWithoutResponseLeak(t *testing.T) {
	data := &sourceHTTPStore{}
	writer := &fakeSecretWriter{}
	router := NewRouterWithSecrets(app.New(data), writer)

	body := "{\"name\":\"GitHub\",\"kind\":\"github\",\"credential\":\"source-test-value\"}"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/source-connections", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if string(writer.value) != "source-test-value" {
		t.Fatalf("stored value = %q", writer.value)
	}
	wantRef := "source-connection:" + sourceConnectionTestID
	if writer.ref != wantRef {
		t.Fatalf("stored ref = %q, want %q", writer.ref, wantRef)
	}
	if data.connection.CredentialRef == nil || *data.connection.CredentialRef != wantRef {
		t.Fatalf("connection ref = %#v", data.connection.CredentialRef)
	}
	if strings.Contains(rec.Body.String(), "source-test-value") || strings.Contains(rec.Body.String(), wantRef) {
		t.Fatalf("secret material leaked: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"credentialConfigured\":true") {
		t.Fatalf("safe credential status missing: %s", rec.Body.String())
	}
}

func TestSourceRepositoryListReturnsSafePersistedMetadata(t *testing.T) {
	data := &sourceHTTPStore{
		connection: store.SourceConnection{ID: sourceConnectionTestID, Kind: store.SourceProviderGitHub, Name: "GitHub", Enabled: true, HealthStatus: "UNKNOWN"},
		repositories: []store.SourceRepository{{ID: otherID, SourceConnectionID: sourceConnectionTestID, ExternalID: "42", Namespace: "acme", Name: "repo", Path: "acme/repo", WebURL: "web", DefaultBranch: "main"}},
	}
	router := NewRouter(app.New(data))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/source-connections/"+sourceConnectionTestID+"/repositories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"externalId\":\"42\"") || !strings.Contains(rec.Body.String(), "\"defaultBranch\":\"main\"") {
		t.Fatalf("repository metadata missing: %s", rec.Body.String())
	}
}


type sourceCredentialCaptureStore struct {
	record secrets.Record
}

func (s *sourceCredentialCaptureStore) PutSecret(_ context.Context, record secrets.Record) (secrets.Record, error) {
	record.ID = "secret-1"
	s.record = record
	return record, nil
}

func (s *sourceCredentialCaptureStore) GetSecret(_ context.Context, projectID *string, ref string) (secrets.Record, error) {
	if s.record.Ref != ref {
		return secrets.Record{}, secrets.ErrNotFound
	}
	if (s.record.ProjectID == nil) != (projectID == nil) {
		return secrets.Record{}, secrets.ErrNotFound
	}
	if projectID != nil && s.record.ProjectID != nil && *projectID != *s.record.ProjectID {
		return secrets.Record{}, secrets.ErrNotFound
	}
	return s.record, nil
}

func TestSourceConnectionCredentialUsesExistingEncryptionBoundary(t *testing.T) {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	cipher, err := secrets.NewAESGCM(1, map[int][]byte{1: key})
	if err != nil {
		t.Fatal(err)
	}
	secretStore := &sourceCredentialCaptureStore{}
	secretService, err := secrets.NewService(secretStore, cipher)
	if err != nil {
		t.Fatal(err)
	}
	data := &sourceHTTPStore{}
	router := NewRouterWithSecrets(app.New(data), secretService)

	const plaintext = "recognizable-source-credential"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/source-connections", strings.NewReader(
		`{"name":"GitHub","kind":"github","credential":"`+plaintext+`"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(secretStore.record.Ciphertext) == 0 || bytes.Contains(secretStore.record.Ciphertext, []byte(plaintext)) {
		t.Fatalf("credential was not encrypted before persistence: %q", secretStore.record.Ciphertext)
	}
	if strings.Contains(rec.Body.String(), plaintext) || strings.Contains(rec.Body.String(), secretStore.record.Ref) {
		t.Fatalf("credential material leaked through HTTP: %s", rec.Body.String())
	}
	resolved, err := secretService.Resolve(t.Context(), secrets.Scope{}, secretStore.record.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(resolved) != plaintext {
		t.Fatalf("resolved credential = %q", resolved)
	}
}
