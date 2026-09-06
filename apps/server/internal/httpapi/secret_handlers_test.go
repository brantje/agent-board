package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
)

const testSecretWriteToken = "0123456789abcdef0123456789abcdef"

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

type scopedSecretWriteAuthorizer struct {
	allowedProject string
	seenProject    *string
}

func (a *scopedSecretWriteAuthorizer) AuthorizeSecretWrite(_ *http.Request, projectID *string) bool {
	if projectID == nil {
		a.seenProject = nil
		return false
	}
	value := *projectID
	a.seenProject = &value
	return value == a.allowedProject
}

func deploymentAuthorizedRouter(t *testing.T, writer app.SecretWriter) http.Handler {
	t.Helper()
	authorizer, err := NewDeploymentSecretWriteAuthorizer(testSecretWriteToken)
	if err != nil {
		t.Fatal(err)
	}
	return NewRouterWithSecrets(app.New(&fakeControlPlaneStore{}), writer, authorizer)
}

func authorizeSecretRequest(req *http.Request) {
	req.Header.Set(SecretWriteCapabilityHeader, testSecretWriteToken)
}

func TestDeploymentSecretWriteAuthorizerRejectsWeakAndInvalidCapabilities(t *testing.T) {
	if _, err := NewDeploymentSecretWriteAuthorizer("short"); !errors.Is(err, ErrSecretWriteCapabilityInvalid) {
		t.Fatalf("weak token error=%v", err)
	}
	authorizer, err := NewDeploymentSecretWriteAuthorizer(testSecretWriteToken)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/secrets", nil)
	if authorizer.AuthorizeSecretWrite(req, nil) {
		t.Fatal("missing capability unexpectedly authorized")
	}
	req.Header.Set(SecretWriteCapabilityHeader, "wrong-token-value-that-is-long-enough")
	if authorizer.AuthorizeSecretWrite(req, nil) {
		t.Fatal("wrong capability unexpectedly authorized")
	}
	authorizeSecretRequest(req)
	if !authorizer.AuthorizeSecretWrite(req, nil) {
		t.Fatal("configured capability was rejected")
	}
}

func TestSecretAPIRequiresAuthorizationBeforeWriting(t *testing.T) {
	writer := &fakeSecretWriter{}
	router := NewRouterWithSecrets(app.New(&fakeControlPlaneStore{}), writer)
	req := httptest.NewRequest(http.MethodPut, "/api/secrets", strings.NewReader(`{"ref":"provider-token","value":"plain"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || writer.ref != "" {
		t.Fatalf("status=%d writer=%+v body=%s", rec.Code, writer, rec.Body.String())
	}
}

func TestSecretAPIWritesGlobalSecretWithoutEchoingPlaintext(t *testing.T) {
	writer := &fakeSecretWriter{}
	router := deploymentAuthorizedRouter(t, writer)
	secret := "canary-api-secret"
	req := httptest.NewRequest(http.MethodPut, "/api/secrets", strings.NewReader(`{"ref":"provider-token","value":"`+secret+`"}`))
	authorizeSecretRequest(req)
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

func TestSecretAPIWritesProjectScopedSecretAndEnforcesTargetAuthorization(t *testing.T) {
	writer := &fakeSecretWriter{}
	authorizer := &scopedSecretWriteAuthorizer{allowedProject: projectID}
	router := NewRouterWithSecrets(app.New(&fakeControlPlaneStore{}), writer, authorizer)
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+projectID+"/secrets", strings.NewReader(`{"ref":"runtime-token","value":"plain"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || writer.scope.ProjectID == nil || *writer.scope.ProjectID != projectID {
		t.Fatalf("status=%d scope=%+v body=%s", rec.Code, writer.scope, rec.Body.String())
	}
	if authorizer.seenProject == nil || *authorizer.seenProject != projectID {
		t.Fatalf("authorizer did not receive target project: %+v", authorizer.seenProject)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/projects/"+otherID+"/secrets", strings.NewReader(`{"ref":"runtime-token","value":"plain"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized project status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSecretAPIValidatesProjectAfterAuthorization(t *testing.T) {
	writer := &fakeSecretWriter{}
	router := deploymentAuthorizedRouter(t, writer)
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+otherID+"/secrets", strings.NewReader(`{"ref":"runtime-token","value":"plain"}`))
	authorizeSecretRequest(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign/missing project status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSecretAPIRejectsMissingValueAndIsAbsentWithoutWriter(t *testing.T) {
	router := deploymentAuthorizedRouter(t, &fakeSecretWriter{})
	req := httptest.NewRequest(http.MethodPut, "/api/secrets", strings.NewReader(`{"ref":"token","value":""}`))
	authorizeSecretRequest(req)
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
