package docker

import (
	"context"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type errorMoby struct {
	*fakeMoby
	pullErr   error
	startErr  error
	stopErr   error
	removeErr error
}

func (f *errorMoby) ImagePull(ctx context.Context, image string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	return f.fakeMoby.ImagePull(ctx, image, options)
}

func (f *errorMoby) ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error) {
	f.startCalls++
	return client.ContainerStartResult{}, f.startErr
}

func (f *errorMoby) ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error) {
	f.stopCalls++
	return client.ContainerStopResult{}, f.stopErr
}

func (f *errorMoby) ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	f.removeCalls++
	return client.ContainerRemoveResult{}, f.removeErr
}

func TestDockerRuntimeConstructionCloseAndImageErrors(t *testing.T) {
	if _, err := newWithClient(nil); err == nil {
		t.Fatal("newWithClient() unexpectedly accepted nil client")
	}
	if err := (&Runtime{}).Close(); err != nil {
		t.Fatalf("Close() without closer error=%v", err)
	}
	if err := (*Runtime)(nil).Close(); err != nil {
		t.Fatalf("nil Close() error=%v", err)
	}
	closeErr := errors.New("close failed")
	closeCalls := 0
	runtime := &Runtime{close: func() error { closeCalls++; return closeErr }}
	if err := runtime.Close(); !errors.Is(err, closeErr) || closeCalls != 1 {
		t.Fatalf("Close() calls=%d err=%v", closeCalls, err)
	}

	fake := &fakeMoby{}
	runtime, _ = newWithClient(fake)
	if err := runtime.ensureImage(context.Background(), "present:image"); err != nil || fake.pullCalls != 0 {
		t.Fatalf("ensureImage(present) pullCalls=%d err=%v", fake.pullCalls, err)
	}

	inspectErr := errors.New("inspect failed")
	fake = &fakeMoby{imageInspectErr: inspectErr}
	runtime, _ = newWithClient(fake)
	if err := runtime.ensureImage(context.Background(), "bad:image"); !errors.Is(err, inspectErr) {
		t.Fatalf("ensureImage(inspect error)=%v", err)
	}

	pullErr := errors.New("pull failed")
	failing := &errorMoby{fakeMoby: &fakeMoby{imageInspectErr: cerrdefs.ErrNotFound}, pullErr: pullErr}
	runtime, _ = newWithClient(failing)
	if err := runtime.ensureImage(context.Background(), "missing:image"); !errors.Is(err, pullErr) {
		t.Fatalf("ensureImage(pull error)=%v", err)
	}

	waitErr := errors.New("pull wait failed")
	fake = &fakeMoby{imageInspectErr: cerrdefs.ErrNotFound, pullWaitErr: waitErr}
	runtime, _ = newWithClient(fake)
	if err := runtime.ensureImage(context.Background(), "missing:image"); !errors.Is(err, waitErr) {
		t.Fatalf("ensureImage(wait error)=%v", err)
	}
}

func TestDockerCreateRaceAndInspectionErrors(t *testing.T) {
	spec := dockerSpec()
	inspectErr := errors.New("inspect existing failed")
	fake := &fakeMoby{inspectFn: func(string) (client.ContainerInspectResult, error) {
		return client.ContainerInspectResult{}, inspectErr
	}}
	runtime, _ := newWithClient(fake)
	if _, err := runtime.Create(context.Background(), spec); !errors.Is(err, inspectErr) {
		t.Fatalf("Create(existing inspect error)=%v", err)
	}

	inspectCalls := 0
	fake = &fakeMoby{}
	fake.inspectFn = func(ref string) (client.ContainerInspectResult, error) {
		if ref == containerName(spec.RuntimeInstanceID) {
			inspectCalls++
			if inspectCalls == 1 {
				return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
			}
			return ownedInspect(spec, "race-id", "created"), nil
		}
		return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
	}
	fake.createFn = func(client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
		return client.ContainerCreateResult{}, cerrdefs.ErrAlreadyExists
	}
	runtime, _ = newWithClient(fake)
	handle, err := runtime.Create(context.Background(), spec)
	if err != nil || handle.ExternalID != "race-id" || fake.createCalls != 1 || inspectCalls != 2 {
		t.Fatalf("Create(race) handle=%+v createCalls=%d inspectCalls=%d err=%v", handle, fake.createCalls, inspectCalls, err)
	}

	fake = &fakeMoby{}
	fake.inspectFn = func(ref string) (client.ContainerInspectResult, error) {
		if ref == containerName(spec.RuntimeInstanceID) {
			return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
		}
		return client.ContainerInspectResult{}, errors.New("created inspect failed")
	}
	runtime, _ = newWithClient(fake)
	if _, err := runtime.Create(context.Background(), spec); err == nil {
		t.Fatal("Create() unexpectedly ignored created-container inspect error")
	}
}

func TestDockerRecoverAndOwnedInspectionErrorBranches(t *testing.T) {
	spec := dockerSpec()
	fake := &fakeMoby{}
	runtime, _ := newWithClient(fake)
	if _, _, err := runtime.Recover(context.Background(), spec); !errors.Is(err, runtimepkg.ErrNotFound) {
		t.Fatalf("Recover(not found)=%v", err)
	}

	inspectErr := errors.New("recover inspect failed")
	fake = &fakeMoby{inspectFn: func(string) (client.ContainerInspectResult, error) {
		return client.ContainerInspectResult{}, inspectErr
	}}
	runtime, _ = newWithClient(fake)
	if _, _, err := runtime.Recover(context.Background(), spec); !errors.Is(err, inspectErr) {
		t.Fatalf("Recover(inspect error)=%v", err)
	}

	fake = &fakeMoby{inspectFn: func(string) (client.ContainerInspectResult, error) {
		inspected := ownedInspect(spec, "container-id", "running")
		inspected.Container.Config.Labels[labelProject] = "other-project"
		return inspected, nil
	}}
	runtime, _ = newWithClient(fake)
	if _, _, err := runtime.Recover(context.Background(), spec); !errors.Is(err, runtimepkg.ErrOwnershipMismatch) {
		t.Fatalf("Recover(ownership mismatch)=%v", err)
	}

	handle, _ := handleFromSpec(spec, "")
	seenRef := ""
	fake = &fakeMoby{inspectFn: func(ref string) (client.ContainerInspectResult, error) {
		seenRef = ref
		return ownedInspect(spec, "container-id", "running"), nil
	}}
	runtime, _ = newWithClient(fake)
	if _, _, err := runtime.inspectOwned(context.Background(), handle); err != nil || seenRef != containerName(spec.RuntimeInstanceID) {
		t.Fatalf("inspectOwned() ref=%q err=%v", seenRef, err)
	}

	if _, _, err := runtime.inspectOwned(context.Background(), runtimepkg.Handle{Metadata: []byte(`{}`)}); !errors.Is(err, runtimepkg.ErrOwnershipMismatch) {
		t.Fatalf("inspectOwned(invalid metadata)=%v", err)
	}
}

func TestDockerLifecycleAPIErrorsAndMissingContainers(t *testing.T) {
	spec := dockerSpec()
	handle, _ := handleFromSpec(spec, "container-id")

	fake := &fakeMoby{}
	runtime, _ := newWithClient(fake)
	if err := runtime.Stop(context.Background(), handle, runtimepkg.StopReasonRequested); err != nil {
		t.Fatalf("Stop(not found)=%v", err)
	}
	if err := runtime.Destroy(context.Background(), handle); err != nil {
		t.Fatalf("Destroy(not found)=%v", err)
	}

	startErr := errors.New("start failed")
	failing := &errorMoby{fakeMoby: &fakeMoby{inspectFn: func(string) (client.ContainerInspectResult, error) {
		return ownedInspect(spec, "container-id", "created"), nil
	}}, startErr: startErr}
	runtime, _ = newWithClient(failing)
	if err := runtime.Start(context.Background(), handle); !errors.Is(err, startErr) || failing.startCalls != 1 {
		t.Fatalf("Start() calls=%d err=%v", failing.startCalls, err)
	}

	stopErr := errors.New("stop failed")
	failing = &errorMoby{fakeMoby: &fakeMoby{inspectFn: func(string) (client.ContainerInspectResult, error) {
		return ownedInspect(spec, "container-id", "running"), nil
	}}, stopErr: stopErr}
	runtime, _ = newWithClient(failing)
	if err := runtime.Stop(context.Background(), handle, runtimepkg.StopReasonRequested); !errors.Is(err, stopErr) || failing.stopCalls != 1 {
		t.Fatalf("Stop() calls=%d err=%v", failing.stopCalls, err)
	}

	failing = &errorMoby{fakeMoby: &fakeMoby{inspectFn: func(string) (client.ContainerInspectResult, error) {
		return ownedInspect(spec, "container-id", "running"), nil
	}}, stopErr: stopErr}
	runtime, _ = newWithClient(failing)
	if err := runtime.Destroy(context.Background(), handle); err != nil || failing.stopCalls != 1 || failing.removeCalls != 1 {
		t.Fatalf("Destroy(stop error, forced remove succeeds) stopCalls=%d removeCalls=%d err=%v", failing.stopCalls, failing.removeCalls, err)
	}

	removeErr := errors.New("remove failed")
	failing = &errorMoby{fakeMoby: &fakeMoby{inspectFn: func(string) (client.ContainerInspectResult, error) {
		return ownedInspect(spec, "container-id", "running"), nil
	}}, stopErr: stopErr, removeErr: removeErr}
	runtime, _ = newWithClient(failing)
	if err := runtime.Destroy(context.Background(), handle); !errors.Is(err, stopErr) || !errors.Is(err, removeErr) || failing.removeCalls != 1 {
		t.Fatalf("Destroy(stop and remove errors) stopCalls=%d removeCalls=%d err=%v", failing.stopCalls, failing.removeCalls, err)
	}

	failing = &errorMoby{fakeMoby: &fakeMoby{inspectFn: func(string) (client.ContainerInspectResult, error) {
		return ownedInspect(spec, "container-id", "exited"), nil
	}}, removeErr: removeErr}
	runtime, _ = newWithClient(failing)
	if err := runtime.Destroy(context.Background(), handle); !errors.Is(err, removeErr) || failing.removeCalls != 1 {
		t.Fatalf("Destroy(remove error) calls=%d err=%v", failing.removeCalls, err)
	}
}

func TestVerifyContainerRejectsUnsafeOrMismatchedShapes(t *testing.T) {
	spec := dockerSpec()
	for _, tc := range []struct {
		name   string
		mutate func(*container.InspectResponse, *handleMetadata)
	}{
		{"incomplete", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.Config = nil }},
		{"label", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.Config.Labels[labelRuntime] = "other" }},
		{"image", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.Config.Image = "other:image" }},
		{"working directory", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.Config.WorkingDir = "/tmp" }},
		{"privileged", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.HostConfig.Privileged = true }},
		{"host pid", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.HostConfig.PidMode = container.PidMode("host") }},
		{"capability", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.HostConfig.CapAdd = append(inspected.HostConfig.CapAdd, "SYS_ADMIN") }},
		{"device", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.HostConfig.Devices = []container.DeviceMapping{{PathOnHost: "/dev/null"}} }},
		{"mount count", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.Mounts = nil }},
		{"mount shape", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.Mounts[0].RW = false }},
		{"docker socket", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.Mounts[0].Source = "/var/run/docker.sock" }},
		{"none network", func(inspected *container.InspectResponse, _ *handleMetadata) { inspected.HostConfig.NetworkMode = container.NetworkMode("bridge"); inspected.Config.NetworkDisabled = false }},
		{"unsupported network", func(_ *container.InspectResponse, meta *handleMetadata) { meta.Network = runtimepkg.NetworkRestricted }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspected := ownedInspect(spec, "container-id", "running").Container
			meta := metadataFromSpec(spec)
			tc.mutate(&inspected, &meta)
			if err := verifyContainer(inspected, meta); !errors.Is(err, runtimepkg.ErrOwnershipMismatch) {
				t.Fatalf("verifyContainer() error=%v", err)
			}
		})
	}

	outbound := dockerSpec()
	outbound.Network = runtimepkg.NetworkOutbound
	inspected := ownedInspect(outbound, "container-id", "running").Container
	inspected.HostConfig.NetworkMode = container.NetworkMode("host")
	if err := verifyContainer(inspected, metadataFromSpec(outbound)); !errors.Is(err, runtimepkg.ErrOwnershipMismatch) {
		t.Fatalf("verifyContainer(outbound host)=%v", err)
	}
}

func TestDockerMetadataAndStateMappingBranches(t *testing.T) {
	if _, err := decodeHandleMetadata(nil); !errors.Is(err, runtimepkg.ErrOwnershipMismatch) {
		t.Fatalf("decodeHandleMetadata(nil)=%v", err)
	}
	if _, err := decodeHandleMetadata([]byte(`{"containerName":"only-name"}`)); !errors.Is(err, runtimepkg.ErrOwnershipMismatch) {
		t.Fatalf("decodeHandleMetadata(incomplete)=%v", err)
	}

	for _, tc := range []struct {
		status string
		want   runtimepkg.State
	}{
		{"created", runtimepkg.StateProvisioning},
		{"running", runtimepkg.StateRunning},
		{"paused", runtimepkg.StateRunning},
		{"restarting", runtimepkg.StateStarting},
		{"removing", runtimepkg.StateStopping},
		{"exited", runtimepkg.StateStopped},
		{"dead", runtimepkg.StateFailed},
		{"unknown", runtimepkg.StateFailed},
	} {
		inspected := container.InspectResponse{ID: "container-id", State: &container.State{Status: container.ContainerState(tc.status)}}
		got := inspectionFromContainer(inspected)
		if got.ExternalID != "container-id" || got.State != tc.want {
			t.Fatalf("inspectionFromContainer(%q)=%+v", tc.status, got)
		}
	}
	if got := inspectionFromContainer(container.InspectResponse{ID: "container-id"}); got.State != runtimepkg.StateFailed {
		t.Fatalf("inspectionFromContainer(nil state)=%+v", got)
	}
}
