package repository

import (
	"testing"
)

func TestSettingsFromEnvPrefersMountPath(t *testing.T) {
	t.Setenv("AGENT_BOARD_REPOSITORY_ROOTS", "/repositories:/mnt/other")
	t.Setenv("AGENT_BOARD_REPOSITORY_MOUNT_PATH", "/repositories")
	got := SettingsFromEnv()
	if got.DefaultRepositoryPath != "/repositories" {
		t.Fatalf("DefaultRepositoryPath = %q, want /repositories", got.DefaultRepositoryPath)
	}
	if len(got.RepositoryRoots) != 2 {
		t.Fatalf("RepositoryRoots = %#v, want two roots", got.RepositoryRoots)
	}
}

func TestSettingsFromEnvFallsBackToFirstRoot(t *testing.T) {
	t.Setenv("AGENT_BOARD_REPOSITORY_MOUNT_PATH", "")
	t.Setenv("AGENT_BOARD_REPOSITORY_ROOTS", "/repositories")
	got := SettingsFromEnv()
	if got.DefaultRepositoryPath != "/repositories" {
		t.Fatalf("DefaultRepositoryPath = %q, want /repositories", got.DefaultRepositoryPath)
	}
}
