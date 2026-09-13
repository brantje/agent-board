package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}
	if got := strings.TrimSpace(response.Body.String()); got != `{"status":"ok"}` {
		t.Fatalf("unexpected response body: %q", got)
	}
}

func TestProjectAccessRouterFailsClosedWithoutAuth(t *testing.T) {
	t.Parallel()

	handler := newRouterWithProjectAccess(
		&app.Service{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&app.ProjectAccessService{},
		nil,
	)
	request := httptest.NewRequest(http.MethodGet, "/api/projects/00000000-0000-0000-0000-000000000000/access/effective-role", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected project routes to be unavailable without auth, got status %d body=%s", response.Code, response.Body.String())
	}
}
