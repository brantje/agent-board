package docker

import (
	"context"
	"errors"
	"io"
	"iter"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

type fakeMoby struct {
	imageInspectErr error
	pullWaitErr     error
	inspectFn       func(string) (client.ContainerInspectResult, error)
	createFn        func(client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	createCalls     int
	startCalls      int
	stopCalls       int
	removeCalls     int
	pullCalls       int
}

func (f *fakeMoby) ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	return client.ImageInspectResult{}, f.imageInspectErr
}
func (f *fakeMoby) ImagePull(context.Context, string, client.ImagePullOptions) (client.ImagePullResponse, error) {
	f.pullCalls++
	return &fakePullResponse{waitErr: f.pullWaitErr}, nil
}
func (f *fakeMoby) ContainerCreate(_ context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	f.createCalls++
	if f.createFn != nil {
		return f.createFn(options)
	}
	return client.ContainerCreateResult{ID: "created-id"}, nil
}
func (f *fakeMoby) ContainerInspect(_ context.Context, ref string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if f.inspectFn != nil {
		return f.inspectFn(ref)
	}
	return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
}
func (f *fakeMoby) ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error) {
	f.startCalls++
	return client.ContainerStartResult{}, nil
}
func (f *fakeMoby) ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error) {
	f.stopCalls++
	return client.ContainerStopResult{}, nil
}
func (f *fakeMoby) ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	f.removeCalls++
	return client.ContainerRemoveResult{}, nil
}

type fakePullResponse struct{ waitErr error }

func (f *fakePullResponse) Read([]byte) (int, error)   { return 0, io.EOF }
func (f *fakePullResponse) Close() error               { return nil }
func (f *fakePullResponse) Wait(context.Context) error { return f.waitErr }
func (f *fakePullResponse) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}

func dockerSpec() runtimepkg.RuntimeSpec {
	cpu, pids := 750, 64
	memory := int64(256 << 20)
	return runtimepkg.RuntimeSpec{
		RuntimeInstanceID: "instance-1",
		ProjectID:         "project-1",
		IssueID:           "issue-1",
		WorkspaceID:       "workspace-1",
		RuntimeID:         "runtime-1",
		Image:             "agent-board-runtime:test",
		WorkingDirectory:  "/workspace",
		Resources: runtimepkg.ResourcePolicy{
			CPULimitMillis:   &cpu,
			MemoryLimitBytes: &memory,
			PIDLimit:         &pids,
		},
		Workspace: runtimepkg.WorkspaceMount{
			WorkspaceID: "workspace-1",
			Source:      "/var/lib/agent-board/workspaces/workspace-1",
			Target:      runtimepkg.WorkspaceTarget,
		},
		Network: runtimepkg.NetworkNone,
		Labels:  map[string]string{"agent-board.test": "true"},
	}
}

func ownedInspect(spec runtimepkg.RuntimeSpec, id, state string) client.ContainerInspectResult {
	options := buildCreateOptions(spec)
	return client.ContainerInspectResult{Container: container.InspectResponse{
		ID:         id,
		Name:       "/" + options.Name,
		Config:     options.Config,
		HostConfig: options.HostConfig,
		State:      &container.State{Status: container.ContainerState(state), Running: state == "running"},
		Mounts: []container.MountPoint{{
			Type:        mount.TypeBind,
			Source:      spec.Workspace.Source,
			Destination: runtimepkg.WorkspaceTarget,
			RW:          true,
		}},
	}}
}

func TestBuildCreateOptionsEnforcesWorkspaceResourcesAndIsolation(t *testing.T) {
	spec := dockerSpec()
	options := buildCreateOptions(spec)
	if options.Name != containerName(spec.RuntimeInstanceID) {
		t.Fatalf("container name=%q", options.Name)
	}
	if options.Config.WorkingDir != runtimepkg.WorkspaceTarget || !options.Config.NetworkDisabled {
		t.Fatalf("config=%+v", options.Config)
	}
	if options.HostConfig.Privileged || options.HostConfig.PublishAllPorts || !options.HostConfig.NetworkMode.IsNone() {
		t.Fatalf("unsafe host config=%+v", options.HostConfig)
	}
	if len(options.HostConfig.Mounts) != 1 {
		t.Fatalf("mounts=%+v", options.HostConfig.Mounts)
	}
	workspace := options.HostConfig.Mounts[0]
	if workspace.Type != mount.TypeBind || workspace.Source != spec.Workspace.Source || workspace.Target != runtimepkg.WorkspaceTarget || workspace.ReadOnly {
		t.Fatalf("workspace mount=%+v", workspace)
	}
	if options.HostConfig.NanoCPUs != 750_000_000 || options.HostConfig.Memory != 256<<20 || options.HostConfig.PidsLimit == nil || *options.HostConfig.PidsLimit != 64 {
		t.Fatalf("resources=%+v", options.HostConfig.Resources)
	}
	if options.Config.Labels[labelWorkspace] != spec.WorkspaceID || options.Config.Labels[labelRuntimeInstance] != spec.RuntimeInstanceID {
		t.Fatalf("labels=%v", options.Config.Labels)
	}
	for _, env := range options.Config.Env {
		if env == "DOCKER_HOST=/var/run/docker.sock" {
			t.Fatalf("Docker host leaked in env: %v", options.Config.Env)
		}
	}
}

func TestBuildCreateOptionsAllowsOutboundWithoutHostNetworking(t *testing.T) {
	spec := dockerSpec()
	spec.Network = runtimepkg.NetworkOutbound
	options := buildCreateOptions(spec)
	if options.Config.NetworkDisabled || options.HostConfig.NetworkMode.IsHost() || options.HostConfig.NetworkMode.IsNone() {
		t.Fatalf("network config=%+v", options.HostConfig.NetworkMode)
	}
}

func TestDockerCreatePullsMissingImageAndPersistsDeterministicIdentity(t *testing.T) {
	spec := dockerSpec()
	fake := &fakeMoby{imageInspectErr: cerrdefs.ErrNotFound}
	fake.inspectFn = func(ref string) (client.ContainerInspectResult, error) {
		if ref == containerName(spec.RuntimeInstanceID) {
			return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
		}
		if ref == "created-id" {
			return ownedInspect(spec, "created-id", "created"), nil
		}
		return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
	}
	runtime, err := newWithClient(fake)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := runtime.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create() error=%v", err)
	}
	if handle.ExternalID != "created-id" || fake.pullCalls != 1 || fake.createCalls != 1 {
		t.Fatalf("handle=%+v pullCalls=%d createCalls=%d", handle, fake.pullCalls, fake.createCalls)
	}
	meta, err := decodeHandleMetadata(handle.Metadata)
	if err != nil || meta.ContainerName != containerName(spec.RuntimeInstanceID) || meta.WorkspaceID != spec.WorkspaceID {
		t.Fatalf("metadata=%+v err=%v", meta, err)
	}
}

func TestDockerCreateReusesOwnedDeterministicContainer(t *testing.T) {
	spec := dockerSpec()
	fake := &fakeMoby{}
	fake.inspectFn = func(ref string) (client.ContainerInspectResult, error) {
		if ref == containerName(spec.RuntimeInstanceID) {
			return ownedInspect(spec, "existing-id", "created"), nil
		}
		return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
	}
	runtime, _ := newWithClient(fake)
	handle, err := runtime.Create(context.Background(), spec)
	if err != nil || handle.ExternalID != "existing-id" || fake.createCalls != 0 {
		t.Fatalf("handle=%+v createCalls=%d err=%v", handle, fake.createCalls, err)
	}
}

func TestDockerCreateRejectsMismatchedExistingContainer(t *testing.T) {
	spec := dockerSpec()
	fake := &fakeMoby{}
	fake.inspectFn = func(string) (client.ContainerInspectResult, error) {
		inspected := ownedInspect(spec, "existing-id", "created")
		inspected.Container.Config.Labels[labelWorkspace] = "other-workspace"
		return inspected, nil
	}
	runtime, _ := newWithClient(fake)
	if _, err := runtime.Create(context.Background(), spec); !errors.Is(err, runtimepkg.ErrOwnershipMismatch) {
		t.Fatalf("Create() error=%v, want ErrOwnershipMismatch", err)
	}
}

func TestDockerRejectsUnsupportedExecutablePolicies(t *testing.T) {
	runtime, _ := newWithClient(&fakeMoby{})
	spec := dockerSpec()
	spec.Network = runtimepkg.NetworkRestricted
	if _, err := runtime.Create(context.Background(), spec); !errors.Is(err, runtimepkg.ErrUnsupportedPolicy) {
		t.Fatalf("restricted Create() error=%v", err)
	}
	spec = dockerSpec()
	timeout := 30
	spec.Resources.TimeoutSeconds = &timeout
	if _, err := runtime.Create(context.Background(), spec); !errors.Is(err, runtimepkg.ErrUnsupportedPolicy) {
		t.Fatalf("timeout Create() error=%v", err)
	}
}

func TestDockerLifecycleIsIdempotentAndOwnershipChecked(t *testing.T) {
	spec := dockerSpec()
	handle, _ := handleFromSpec(spec, "container-id")
	state := "running"
	fake := &fakeMoby{}
	fake.inspectFn = func(string) (client.ContainerInspectResult, error) {
		return ownedInspect(spec, "container-id", state), nil
	}
	runtime, _ := newWithClient(fake)
	if err := runtime.Start(context.Background(), handle); err != nil || fake.startCalls != 0 {
		t.Fatalf("idempotent Start() err=%v calls=%d", err, fake.startCalls)
	}
	inspection, err := runtime.Inspect(context.Background(), handle)
	if err != nil || inspection.State != runtimepkg.StateRunning {
		t.Fatalf("Inspect()=%+v err=%v", inspection, err)
	}
	if err := runtime.Stop(context.Background(), handle, runtimepkg.StopReasonRequested); err != nil || fake.stopCalls != 1 {
		t.Fatalf("Stop() err=%v calls=%d", err, fake.stopCalls)
	}
	state = "exited"
	if err := runtime.Stop(context.Background(), handle, runtimepkg.StopReasonRequested); err != nil || fake.stopCalls != 1 {
		t.Fatalf("idempotent stopped Stop() err=%v calls=%d", err, fake.stopCalls)
	}
	if err := runtime.Destroy(context.Background(), handle); err != nil || fake.removeCalls != 1 {
		t.Fatalf("Destroy() err=%v removeCalls=%d", err, fake.removeCalls)
	}
}

func TestDockerRecoverFindsContainerWithoutPersistedExternalID(t *testing.T) {
	spec := dockerSpec()
	fake := &fakeMoby{}
	fake.inspectFn = func(ref string) (client.ContainerInspectResult, error) {
		if ref != containerName(spec.RuntimeInstanceID) {
			t.Fatalf("Recover inspect ref=%q", ref)
		}
		return ownedInspect(spec, "recovered-id", "running"), nil
	}
	runtime, _ := newWithClient(fake)
	handle, inspection, err := runtime.Recover(context.Background(), spec)
	if err != nil || handle.ExternalID != "recovered-id" || inspection.State != runtimepkg.StateRunning {
		t.Fatalf("Recover() handle=%+v inspection=%+v err=%v", handle, inspection, err)
	}
}
