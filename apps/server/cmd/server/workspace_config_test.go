package main

import "testing"

func TestConfiguredWorkspaceRoot(t *testing.T) {
	t.Setenv("AGENT_BOARD_WORKSPACE_ROOT", "")
	if got := configuredWorkspaceRoot(); got != defaultWorkspaceRoot {
		t.Fatalf("configuredWorkspaceRoot() = %q, want default %q", got, defaultWorkspaceRoot)
	}

	const configured = "/tmp/agent-board-workspaces"
	t.Setenv("AGENT_BOARD_WORKSPACE_ROOT", configured)
	if got := configuredWorkspaceRoot(); got != configured {
		t.Fatalf("configuredWorkspaceRoot() = %q, want %q", got, configured)
	}
}
