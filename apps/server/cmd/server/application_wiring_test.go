package main

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
)

func TestControlPlaneHandlerWiresWorkspaceApplicationServices(t *testing.T) {
	databaseURL := os.Getenv("AGENT_BOARD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for integration wiring test")
	}
	repositoryRoot := t.TempDir()
	t.Setenv("AGENT_BOARD_REPOSITORY_ROOTS", repositoryRoot)
	t.Setenv("AGENT_BOARD_WORKSPACE_ROOT", filepath.Join(t.TempDir(), "workspaces"))
	t.Setenv("AGENT_BOARD_SECRET_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))

	handler, closeStore, err := controlPlaneHandler(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("controlPlaneHandler() error = %v", err)
	}
	defer closeStore()
	application, ok := handler.(*applicationHandler)
	if !ok {
		t.Fatalf("handler type = %T, want *applicationHandler", handler)
	}
	if application.Handler == nil || application.services == nil || application.services.ControlPlane == nil || application.services.Workspaces == nil || application.services.RuntimeInstances == nil || application.services.RunnerConnections == nil || application.services.ExecutionSessions == nil || application.services.Redaction == nil || application.services.Secrets == nil {
		t.Fatalf("application services were not fully wired: %+v", application.services)
	}

	// An invalid request is rejected before persistence, so this verifies the
	// production secret route is registered without depending on package-local
	// PostgreSQL test schema initialization order.
	req := httptest.NewRequest(http.MethodPut, "/api/secrets", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("secret route status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReconcileRuntimeInstancesRejectsNonApplicationHandler(t *testing.T) {
	if err := reconcileRuntimeInstances(context.Background(), http.NotFoundHandler()); err == nil {
		t.Fatal("reconcileRuntimeInstances() unexpectedly accepted an unrelated handler")
	}
}

func TestReconcileExecutionSessionsRejectsNonApplicationHandler(t *testing.T) {
	if err := reconcileExecutionSessions(context.Background(), http.NotFoundHandler()); err == nil {
		t.Fatal("reconcileExecutionSessions() unexpectedly accepted an unrelated handler")
	}
}

func TestConfiguredApplicationRejectsRelativeRepositoryRoot(t *testing.T) {
	t.Setenv("AGENT_BOARD_REPOSITORY_ROOTS", "relative/repositories")
	if _, err := configuredApplication(nil); err == nil {
		t.Fatal("configuredApplication() unexpectedly accepted a relative repository root")
	}
}

func TestConfiguredApplicationRejectsMissingRepositoryRoots(t *testing.T) {
	t.Setenv("AGENT_BOARD_REPOSITORY_ROOTS", "")
	if _, err := configuredApplication(nil); !errors.Is(err, repository.ErrNoAuthorizedRoots) {
		t.Fatalf("configuredApplication() error = %v, want ErrNoAuthorizedRoots", err)
	}
}
