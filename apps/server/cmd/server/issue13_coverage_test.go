package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSuperviseHandlesServerFirstOutcomes(t *testing.T) {
	t.Run("clean scheduler shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		schedulerDone := make(chan error, 1)
		go func() {
			<-ctx.Done()
			schedulerDone <- nil
			close(schedulerDone)
		}()
		if code := supervise(ctx, cancel, func() int { return 7 }, schedulerDone); code != 7 {
			t.Fatalf("exit code=%d, want server code 7", code)
		}
	})

	t.Run("scheduler error during server shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		schedulerDone := make(chan error, 1)
		go func() {
			<-ctx.Done()
			schedulerDone <- errors.New("shutdown failed")
			close(schedulerDone)
		}()
		if code := supervise(ctx, cancel, func() int { return 0 }, schedulerDone); code != 1 {
			t.Fatalf("exit code=%d, want 1", code)
		}
	})
}

func TestConfiguredEvidenceAndSchedulerIdentity(t *testing.T) {
	t.Setenv("AGENT_BOARD_EVIDENCE_ROOT", "/tmp/explicit-evidence")
	if got := configuredEvidenceRoot(); got != "/tmp/explicit-evidence" {
		t.Fatalf("explicit evidence root=%q", got)
	}

	t.Setenv("AGENT_BOARD_EVIDENCE_ROOT", "")
	t.Setenv("AGENT_BOARD_WORKSPACE_ROOT", "/tmp/agent-board/workspaces")
	want := filepath.Join("/tmp/agent-board", "evidence")
	if got := configuredEvidenceRoot(); got != want {
		t.Fatalf("workspace-derived evidence root=%q want %q", got, want)
	}

	t.Setenv("AGENT_BOARD_WORKSPACE_ROOT", "")
	if got := configuredEvidenceRoot(); got != defaultEvidenceRoot {
		t.Fatalf("default evidence root=%q", got)
	}

	t.Setenv("AGENT_BOARD_SCHEDULER_OWNER_ID", "test-owner")
	if got := configuredSchedulerOwnerID(); got != "test-owner" {
		t.Fatalf("scheduler owner=%q", got)
	}
}
