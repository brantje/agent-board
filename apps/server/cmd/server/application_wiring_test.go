package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestControlPlaneHandlerWiresWorkspaceApplicationServices(t *testing.T) {
	databaseURL := os.Getenv("AGENT_BOARD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for integration wiring test")
	}
	repositoryRoot := t.TempDir()
	t.Setenv("AGENT_BOARD_REPOSITORY_ROOTS", repositoryRoot)
	t.Setenv("AGENT_BOARD_WORKSPACE_ROOT", filepath.Join(t.TempDir(), "workspaces"))

	handler, closeStore, err := controlPlaneHandler(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("controlPlaneHandler() error = %v", err)
	}
	defer closeStore()
	application, ok := handler.(*applicationHandler)
	if !ok {
		t.Fatalf("handler type = %T, want *applicationHandler", handler)
	}
	if application.Handler == nil || application.services == nil || application.services.ControlPlane == nil || application.services.Workspaces == nil {
		t.Fatalf("application services were not fully wired: %+v", application.services)
	}
}

func TestConfiguredApplicationRejectsRelativeRepositoryRoot(t *testing.T) {
	t.Setenv("AGENT_BOARD_REPOSITORY_ROOTS", "relative/repositories")
	if _, err := configuredApplication(nil); err == nil {
		t.Fatal("configuredApplication() unexpectedly accepted a relative repository root")
	}
}
