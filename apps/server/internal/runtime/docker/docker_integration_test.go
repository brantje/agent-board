package docker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/moby/moby/client"
)

func TestDockerRuntimeIntegration(t *testing.T) {
	if os.Getenv("AGENT_BOARD_TEST_DOCKER") != "1" {
		t.Skip("AGENT_BOARD_TEST_DOCKER=1 is required for live Docker integration")
	}
	image := os.Getenv("AGENT_BOARD_TEST_RUNTIME_IMAGE")
	if image == "" {
		image = "agent-board-runtime-lifecycle-test:ci"
	}
	workspace := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	spec := integrationSpec(image, workspace, "integration-first")
	rt, err := New()
	if err != nil {
		t.Fatalf("New() error=%v", err)
	}
	handle, err := rt.Create(ctx, spec)
	if err != nil {
		_ = rt.Close()
		t.Fatalf("Create() error=%v", err)
	}
	registerRuntimeCleanup(t, handle)
	if err := rt.Start(ctx, handle); err != nil {
		_ = rt.Destroy(ctx, handle)
		_ = rt.Close()
		t.Fatalf("Start() error=%v", err)
	}
	waitForFile(t, filepath.Join(workspace, "persisted"))
	assertDockerPolicy(t, ctx, rt, handle, spec)
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error=%v", err)
	}

	// Simulate a backend restart: a fresh Docker client recovers the same
	// Runtime Instance solely from deterministic server-owned identity.
	restarted, err := New()
	if err != nil {
		t.Fatalf("restart New() error=%v", err)
	}
	defer func() {
		if err := restarted.Close(); err != nil {
			t.Errorf("restart Close() error=%v", err)
		}
	}()
	recovered, inspection, err := restarted.Recover(ctx, spec)
	if err != nil || recovered.ExternalID != handle.ExternalID || inspection.State != runtimepkg.StateRunning {
		t.Fatalf("Recover() handle=%+v inspection=%+v err=%v", recovered, inspection, err)
	}
	if err := restarted.Stop(ctx, recovered, runtimepkg.StopReasonShutdown); err != nil {
		t.Fatalf("Stop() error=%v", err)
	}
	inspection, err = restarted.Inspect(ctx, recovered)
	if err != nil || inspection.State != runtimepkg.StateStopped {
		t.Fatalf("Inspect(stopped)=%+v err=%v", inspection, err)
	}
	if err := restarted.Stop(ctx, recovered, runtimepkg.StopReasonShutdown); err != nil {
		t.Fatalf("idempotent Stop() error=%v", err)
	}
	if err := restarted.Destroy(ctx, recovered); err != nil {
		t.Fatalf("Destroy() error=%v", err)
	}
	if err := restarted.Destroy(ctx, recovered); err != nil {
		t.Fatalf("idempotent Destroy() error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "persisted")); err != nil {
		t.Fatalf("Workspace did not survive Runtime destruction: %v", err)
	}

	// A replacement Runtime Instance may mount the same durable Workspace and
	// must observe state written by the first instance.
	replacementSpec := integrationSpec(image, workspace, "integration-replacement")
	replacement, err := restarted.Create(ctx, replacementSpec)
	if err != nil {
		t.Fatalf("replacement Create() error=%v", err)
	}
	registerRuntimeCleanup(t, replacement)
	if err := restarted.Start(ctx, replacement); err != nil {
		t.Fatalf("replacement Start() error=%v", err)
	}
	waitForFile(t, filepath.Join(workspace, "replacement-sees-state"))
	if err := restarted.Destroy(ctx, replacement); err != nil {
		t.Fatalf("replacement Destroy() error=%v", err)
	}
}

func registerRuntimeCleanup(t *testing.T, handle runtimepkg.Handle) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cleanup, err := New()
		if err != nil {
			t.Errorf("cleanup New() error=%v", err)
			return
		}
		if err := cleanup.Destroy(cleanupCtx, handle); err != nil {
			t.Errorf("cleanup Destroy() error=%v", err)
		}
		if err := cleanup.Close(); err != nil {
			t.Errorf("cleanup Close() error=%v", err)
		}
	})
}

func integrationSpec(image, workspace, instanceID string) runtimepkg.RuntimeSpec {
	cpu, pids := 500, 64
	memory := int64(64 << 20)
	return runtimepkg.RuntimeSpec{
		RuntimeInstanceID: instanceID,
		ProjectID:         "integration-project",
		IssueID:           "integration-issue",
		WorkspaceID:       "integration-workspace",
		RuntimeID:         "integration-runtime",
		Image:             image,
		WorkingDirectory:  runtimepkg.WorkspaceTarget,
		Resources: runtimepkg.ResourcePolicy{
			CPULimitMillis:   &cpu,
			MemoryLimitBytes: &memory,
			PIDLimit:         &pids,
		},
		Workspace: runtimepkg.WorkspaceMount{
			WorkspaceID: "integration-workspace",
			Source:      workspace,
			Target:      runtimepkg.WorkspaceTarget,
		},
		Network: runtimepkg.NetworkNone,
	}
}

func assertDockerPolicy(t *testing.T, ctx context.Context, rt *Runtime, handle runtimepkg.Handle, spec runtimepkg.RuntimeSpec) {
	t.Helper()
	inspected, err := rt.api.ContainerInspect(ctx, handle.ExternalID, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("raw Docker inspect error=%v", err)
	}
	container := inspected.Container
	if container.HostConfig == nil || container.Config == nil {
		t.Fatalf("incomplete Docker inspection: %+v", container)
	}
	if container.Config.Image != spec.Image {
		t.Fatalf("runtime image=%q want=%q", container.Config.Image, spec.Image)
	}
	if container.HostConfig.NanoCPUs != 500_000_000 || container.HostConfig.Memory != 64<<20 || container.HostConfig.PidsLimit == nil || *container.HostConfig.PidsLimit != 64 {
		t.Fatalf("resource policy not enforced: %+v", container.HostConfig.Resources)
	}
	if !container.HostConfig.NetworkMode.IsNone() || !container.Config.NetworkDisabled {
		t.Fatalf("network policy not enforced: mode=%q disabled=%v", container.HostConfig.NetworkMode, container.Config.NetworkDisabled)
	}
	if container.HostConfig.Privileged || container.HostConfig.PublishAllPorts || len(container.HostConfig.CapAdd) != 0 || len(container.HostConfig.Devices) != 0 || len(container.HostConfig.DeviceRequests) != 0 {
		t.Fatalf("unsafe Docker privileges: %+v", container.HostConfig)
	}
	if len(container.Mounts) != 1 || container.Mounts[0].Source != spec.Workspace.Source || container.Mounts[0].Destination != runtimepkg.WorkspaceTarget || !container.Mounts[0].RW {
		t.Fatalf("unexpected mounts: %+v", container.Mounts)
	}
	for _, mounted := range container.Mounts {
		if mounted.Source == "/var/run/docker.sock" || mounted.Destination == "/var/run/docker.sock" {
			t.Fatalf("Docker socket leaked into Runtime Instance: %+v", mounted)
		}
	}
	for _, env := range container.Config.Env {
		if strings.HasPrefix(env, "DOCKER_HOST=") && env != "DOCKER_HOST=" {
			t.Fatalf("Docker daemon endpoint leaked into Runtime Instance: %q", env)
		}
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
