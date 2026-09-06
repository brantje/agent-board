package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
)

type fakeSecretWriter struct {
	scope secrets.Scope
	ref   string
	value []byte
}

func (f *fakeSecretWriter) Put(_ context.Context, scope secrets.Scope, ref string, value []byte) (secrets.Metadata, error) {
	f.scope = scope
	f.ref = ref
	f.value = append([]byte(nil), value...)
	return secrets.Metadata{Ref: ref, ProjectID: scope.ProjectID}, nil
}

func TestSecretAPIWritesGlobalSecretWithoutEchoingPlaintext(t *testing.T) {
	writer := &fakeSecretWriter{}
	router := NewRouterWithSecrets(app.New(&fakeControlPlaneStore{}), writer)
	secret := "canary-api-secret"
	req := httptest.NewRequest(http.MethodPut, "/api/secrets", strings.NewReader(`{"ref":"provider-token","value":"`+secret+`"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if writer.scope.ProjectID != nil || writer.ref != "provider-token" || string(writer.value) != secret {
		t.Fatalf("writer scope=%+v ref=%q value=%q", writer.scope, writer.ref, writer.value)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("response leaked secret: %s", rec.Body.String())
	}
}

func TestSecretAPIWritesProjectScopedSecretAndValidatesProject(t *testing.T) {
	writer := &fakeSecretWriter{}
	router := NewRouterWithSecrets(app.New(&fakeControlPlaneStore{}), writer)
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+projectID+"/secrets", strings.NewReader(`{"ref":"runtime-token","value":"plain"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || writer.scope.ProjectID == nil || *writer.scope.ProjectID != projectID {
		t.Fatalf("status=%d scope=%+v body=%s", rec.Code, writer.scope, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/projects/"+otherID+"/secrets", strings.NewReader(`{"ref":"runtime-token","value":"plain"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign/missing project status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSecretAPIRejectsMissingValueAndIsAbsentWithoutWriter(t *testing.T) {
	router := NewRouterWithSecrets(app.New(&fakeControlPlaneStore{}), &fakeSecretWriter{})
	req := httptest.NewRequest(http.MethodPut, "/api/secrets", strings.NewReader(`{"ref":"token","value":""}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	router = NewRouter(app.New(&fakeControlPlaneStore{}))
	req = httptest.NewRequest(http.MethodPut, "/api/secrets", strings.NewReader(`{"ref":"token","value":"plain"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unconfigured secret route status=%d", rec.Code)
	}
}
