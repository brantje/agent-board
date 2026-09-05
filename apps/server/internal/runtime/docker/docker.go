package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const (
	labelRuntimeInstance = "agent-board.runtime-instance-id"
	labelProject         = "agent-board.project-id"
	labelIssue           = "agent-board.issue-id"
	labelWorkspace       = "agent-board.workspace-id"
	labelRuntime         = "agent-board.runtime-id"
	containerNamePrefix  = "agent-board-runtime-"
)

type mobyAPI interface {
	ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error)
	ImagePull(context.Context, string, client.ImagePullOptions) (client.ImagePullResponse, error)
	ContainerCreate(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
}

type Runtime struct {
	api   mobyAPI
	close func() error
}

type handleMetadata struct {
	ContainerName     string                   `json:"containerName"`
	RuntimeInstanceID string                   `json:"runtimeInstanceId"`
	ProjectID         string                   `json:"projectId"`
	IssueID           string                   `json:"issueId"`
	WorkspaceID       string                   `json:"workspaceId"`
	RuntimeID         string                   `json:"runtimeId"`
	WorkspaceSource   string                   `json:"workspaceSource"`
	WorkingDirectory  string                   `json:"workingDirectory"`
	Network           runtimepkg.NetworkPolicy `json:"network"`
}

func New() (*Runtime, error) {
	api, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	return &Runtime{api: api, close: api.Close}, nil
}

func newWithClient(api mobyAPI) (*Runtime, error) {
	if api == nil {
		return nil, fmt.Errorf("Docker client is required")
	}
	return &Runtime{api: api}, nil
}

func (r *Runtime) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	return r.close()
}

func (r *Runtime) Create(ctx context.Context, spec runtimepkg.RuntimeSpec) (runtimepkg.Handle, error) {
	if err := validateDockerSpec(spec); err != nil {
		return runtimepkg.Handle{}, err
	}
	if err := r.ensureImage(ctx, spec.Image); err != nil {
		return runtimepkg.Handle{}, err
	}

	name := containerName(spec.RuntimeInstanceID)
	if inspected, err := r.api.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		if err := verifyContainer(inspected.Container, metadataFromSpec(spec)); err != nil {
			return runtimepkg.Handle{}, err
		}
		return handleFromSpec(spec, inspected.Container.ID)
	} else if !cerrdefs.IsNotFound(err) {
		return runtimepkg.Handle{}, fmt.Errorf("inspect existing Docker container %s: %w", name, err)
	}

	created, err := r.api.ContainerCreate(ctx, buildCreateOptions(spec))
	if err != nil {
		if cerrdefs.IsAlreadyExists(err) || cerrdefs.IsConflict(err) {
			inspected, inspectErr := r.api.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
			if inspectErr == nil {
				if verifyErr := verifyContainer(inspected.Container, metadataFromSpec(spec)); verifyErr != nil {
					return runtimepkg.Handle{}, verifyErr
				}
				return handleFromSpec(spec, inspected.Container.ID)
			}
		}
		return runtimepkg.Handle{}, fmt.Errorf("create Docker container: %w", err)
	}

	inspected, err := r.api.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
	if err != nil {
		return runtimepkg.Handle{}, fmt.Errorf("inspect created Docker container: %w", err)
	}
	if err := verifyContainer(inspected.Container, metadataFromSpec(spec)); err != nil {
		return runtimepkg.Handle{}, err
	}
	return handleFromSpec(spec, created.ID)
}

// Recover rediscovers a container by the deterministic Runtime Instance name.
// It is used when the database row was committed before Docker creation but the
// process crashed before the external container ID could be persisted.
func (r *Runtime) Recover(ctx context.Context, spec runtimepkg.RuntimeSpec) (runtimepkg.Handle, runtimepkg.Inspection, error) {
	if err := validateDockerSpec(spec); err != nil {
		return runtimepkg.Handle{}, runtimepkg.Inspection{}, err
	}
	inspected, err := r.api.ContainerInspect(ctx, containerName(spec.RuntimeInstanceID), client.ContainerInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return runtimepkg.Handle{}, runtimepkg.Inspection{}, runtimepkg.ErrNotFound
	}
	if err != nil {
		return runtimepkg.Handle{}, runtimepkg.Inspection{}, fmt.Errorf("recover Docker container: %w", err)
	}
	if err := verifyContainer(inspected.Container, metadataFromSpec(spec)); err != nil {
		return runtimepkg.Handle{}, runtimepkg.Inspection{}, err
	}
	handle, err := handleFromSpec(spec, inspected.Container.ID)
	if err != nil {
		return runtimepkg.Handle{}, runtimepkg.Inspection{}, err
	}
	return handle, inspectionFromContainer(inspected.Container), nil
}

func (r *Runtime) Start(ctx context.Context, handle runtimepkg.Handle) error {
	inspected, meta, err := r.inspectOwned(ctx, handle)
	if err != nil {
		return err
	}
	if inspected.State != nil && inspected.State.Running {
		return nil
	}
	if _, err := r.api.ContainerStart(ctx, inspected.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start Docker container %s: %w", meta.ContainerName, err)
	}
	return nil
}

func (r *Runtime) Inspect(ctx context.Context, handle runtimepkg.Handle) (runtimepkg.Inspection, error) {
	inspected, _, err := r.inspectOwned(ctx, handle)
	if err != nil {
		return runtimepkg.Inspection{}, err
	}
	return inspectionFromContainer(inspected), nil
}

func (r *Runtime) Stop(ctx context.Context, handle runtimepkg.Handle, _ runtimepkg.StopReason) error {
	inspected, _, err := r.inspectOwned(ctx, handle)
	if errors.Is(err, runtimepkg.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if inspected.State == nil || !inspected.State.Running {
		return nil
	}
	if _, err := r.api.ContainerStop(ctx, inspected.ID, client.ContainerStopOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("stop Docker container: %w", err)
	}
	return nil
}

func (r *Runtime) Destroy(ctx context.Context, handle runtimepkg.Handle) error {
	inspected, _, err := r.inspectOwned(ctx, handle)
	if errors.Is(err, runtimepkg.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if inspected.State != nil && inspected.State.Running {
		if _, err := r.api.ContainerStop(ctx, inspected.ID, client.ContainerStopOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("stop Docker container before destroy: %w", err)
		}
	}
	if _, err := r.api.ContainerRemove(ctx, inspected.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("destroy Docker container: %w", err)
	}
	return nil
}

func (r *Runtime) ensureImage(ctx context.Context, image string) error {
	if _, err := r.api.ImageInspect(ctx, image); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspect Docker image %q: %w", image, err)
	}
	response, err := r.api.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull Docker image %q: %w", image, err)
	}
	defer response.Close()
	if err := response.Wait(ctx); err != nil {
		return fmt.Errorf("pull Docker image %q: %w", image, err)
	}
	return nil
}

func (r *Runtime) inspectOwned(ctx context.Context, handle runtimepkg.Handle) (container.InspectResponse, handleMetadata, error) {
	meta, err := decodeHandleMetadata(handle.Metadata)
	if err != nil {
		return container.InspectResponse{}, handleMetadata{}, err
	}
	ref := handle.ExternalID
	if strings.TrimSpace(ref) == "" {
		ref = meta.ContainerName
	}
	inspected, err := r.api.ContainerInspect(ctx, ref, client.ContainerInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return container.InspectResponse{}, handleMetadata{}, runtimepkg.ErrNotFound
	}
	if err != nil {
		return container.InspectResponse{}, handleMetadata{}, fmt.Errorf("inspect Docker container: %w", err)
	}
	if err := verifyContainer(inspected.Container, meta); err != nil {
		return container.InspectResponse{}, handleMetadata{}, err
	}
	return inspected.Container, meta, nil
}

func validateDockerSpec(spec runtimepkg.RuntimeSpec) error {
	if err := runtimepkg.ValidateSpec(spec); err != nil {
		return err
	}
	if spec.Network == runtimepkg.NetworkRestricted {
		return fmt.Errorf("%w: Docker restricted network policy is not yet enforceable", runtimepkg.ErrUnsupportedPolicy)
	}
	if spec.Resources.TimeoutSeconds != nil {
		return fmt.Errorf("%w: Docker Runtime Instance wall-clock timeout is not enforceable by this lifecycle", runtimepkg.ErrUnsupportedPolicy)
	}
	return nil
}

func buildCreateOptions(spec runtimepkg.RuntimeSpec) client.ContainerCreateOptions {
	labels := make(map[string]string, len(spec.Labels)+5)
	for key, value := range spec.Labels {
		labels[key] = value
	}
	for key, value := range ownershipLabels(spec) {
		labels[key] = value
	}

	networkMode := container.NetworkMode("bridge")
	networkDisabled := false
	if spec.Network == runtimepkg.NetworkNone {
		networkMode = container.NetworkMode("none")
		networkDisabled = true
	}

	resources := container.Resources{}
	if spec.Resources.CPULimitMillis != nil {
		resources.NanoCPUs = int64(*spec.Resources.CPULimitMillis) * 1_000_000
	}
	if spec.Resources.MemoryLimitBytes != nil {
		resources.Memory = *spec.Resources.MemoryLimitBytes
	}
	if spec.Resources.PIDLimit != nil {
		value := int64(*spec.Resources.PIDLimit)
		resources.PidsLimit = &value
	}

	return client.ContainerCreateOptions{
		Name: containerName(spec.RuntimeInstanceID),
		Config: &container.Config{
			Image:           spec.Image,
			WorkingDir:      spec.WorkingDirectory,
			Labels:          labels,
			NetworkDisabled: networkDisabled,
			Env: []string{
				"DOCKER_HOST=",
				"DOCKER_TLS_VERIFY=",
				"DOCKER_CERT_PATH=",
				"DOCKER_CONFIG=",
			},
		},
		HostConfig: &container.HostConfig{
			NetworkMode:     networkMode,
			Privileged:      false,
			PublishAllPorts: false,
			SecurityOpt:     []string{"no-new-privileges=true"},
			RestartPolicy:   container.RestartPolicy{Name: container.RestartPolicyDisabled},
			Resources:       resources,
			Mounts: []mount.Mount{{
				Type:     mount.TypeBind,
				Source:   spec.Workspace.Source,
				Target:   runtimepkg.WorkspaceTarget,
				ReadOnly: false,
				BindOptions: &mount.BindOptions{
					Propagation: mount.PropagationRPrivate,
				},
			}},
		},
	}
}

func verifyContainer(inspected container.InspectResponse, meta handleMetadata) error {
	if strings.TrimSpace(inspected.ID) == "" || inspected.Config == nil || inspected.HostConfig == nil {
		return fmt.Errorf("%w: Docker container inspection is incomplete", runtimepkg.ErrOwnershipMismatch)
	}
	expectedLabels := map[string]string{
		labelRuntimeInstance: meta.RuntimeInstanceID,
		labelProject:         meta.ProjectID,
		labelIssue:           meta.IssueID,
		labelWorkspace:       meta.WorkspaceID,
		labelRuntime:         meta.RuntimeID,
	}
	for key, value := range expectedLabels {
		if inspected.Config.Labels[key] != value {
			return fmt.Errorf("%w: Docker label %s does not match", runtimepkg.ErrOwnershipMismatch, key)
		}
	}
	if inspected.Config.WorkingDir != meta.WorkingDirectory {
		return fmt.Errorf("%w: Docker working directory does not match", runtimepkg.ErrOwnershipMismatch)
	}
	if inspected.HostConfig.Privileged || inspected.HostConfig.PublishAllPorts || inspected.HostConfig.PidMode.IsHost() || inspected.HostConfig.IpcMode.IsHost() {
		return fmt.Errorf("%w: Docker container has unsafe host privileges", runtimepkg.ErrOwnershipMismatch)
	}
	if len(inspected.HostConfig.Resources.Devices) != 0 || len(inspected.HostConfig.Resources.DeviceRequests) != 0 {
		return fmt.Errorf("%w: Docker container has host device access", runtimepkg.ErrOwnershipMismatch)
	}
	if len(inspected.Mounts) != 1 {
		return fmt.Errorf("%w: Docker container must have exactly one mount", runtimepkg.ErrOwnershipMismatch)
	}
	workspace := inspected.Mounts[0]
	if workspace.Type != mount.TypeBind || workspace.Source != meta.WorkspaceSource || workspace.Destination != runtimepkg.WorkspaceTarget || !workspace.RW {
		return fmt.Errorf("%w: Docker workspace mount does not match", runtimepkg.ErrOwnershipMismatch)
	}
	if workspace.Source == "/var/run/docker.sock" || workspace.Destination == "/var/run/docker.sock" {
		return fmt.Errorf("%w: Docker socket must not be mounted", runtimepkg.ErrOwnershipMismatch)
	}
	switch meta.Network {
	case runtimepkg.NetworkNone:
		if !inspected.HostConfig.NetworkMode.IsNone() || !inspected.Config.NetworkDisabled {
			return fmt.Errorf("%w: Docker none network policy is not enforced", runtimepkg.ErrOwnershipMismatch)
		}
	case runtimepkg.NetworkOutbound:
		if inspected.HostConfig.NetworkMode.IsHost() || inspected.HostConfig.NetworkMode.IsContainer() || inspected.HostConfig.NetworkMode.IsNone() {
			return fmt.Errorf("%w: Docker outbound network policy has unsafe network mode", runtimepkg.ErrOwnershipMismatch)
		}
	default:
		return fmt.Errorf("%w: unsupported persisted network policy", runtimepkg.ErrOwnershipMismatch)
	}
	return nil
}

func ownershipLabels(spec runtimepkg.RuntimeSpec) map[string]string {
	return map[string]string{
		labelRuntimeInstance: spec.RuntimeInstanceID,
		labelProject:         spec.ProjectID,
		labelIssue:           spec.IssueID,
		labelWorkspace:       spec.WorkspaceID,
		labelRuntime:         spec.RuntimeID,
	}
}

func metadataFromSpec(spec runtimepkg.RuntimeSpec) handleMetadata {
	return handleMetadata{
		ContainerName:     containerName(spec.RuntimeInstanceID),
		RuntimeInstanceID: spec.RuntimeInstanceID,
		ProjectID:         spec.ProjectID,
		IssueID:           spec.IssueID,
		WorkspaceID:       spec.WorkspaceID,
		RuntimeID:         spec.RuntimeID,
		WorkspaceSource:   spec.Workspace.Source,
		WorkingDirectory:  spec.WorkingDirectory,
		Network:           spec.Network,
	}
}

func handleFromSpec(spec runtimepkg.RuntimeSpec, externalID string) (runtimepkg.Handle, error) {
	metadata, err := json.Marshal(metadataFromSpec(spec))
	if err != nil {
		return runtimepkg.Handle{}, fmt.Errorf("marshal Docker handle metadata: %w", err)
	}
	return runtimepkg.Handle{ExternalID: externalID, Metadata: metadata}, nil
}

func decodeHandleMetadata(raw json.RawMessage) (handleMetadata, error) {
	var meta handleMetadata
	if len(raw) == 0 || json.Unmarshal(raw, &meta) != nil {
		return handleMetadata{}, fmt.Errorf("%w: invalid Docker handle metadata", runtimepkg.ErrOwnershipMismatch)
	}
	if meta.ContainerName == "" || meta.RuntimeInstanceID == "" || meta.ProjectID == "" || meta.WorkspaceID == "" || meta.RuntimeID == "" || meta.WorkspaceSource == "" || meta.WorkingDirectory == "" {
		return handleMetadata{}, fmt.Errorf("%w: incomplete Docker handle metadata", runtimepkg.ErrOwnershipMismatch)
	}
	return meta, nil
}

func inspectionFromContainer(inspected container.InspectResponse) runtimepkg.Inspection {
	state := runtimepkg.StateFailed
	if inspected.State != nil {
		switch string(inspected.State.Status) {
		case "created":
			state = runtimepkg.StateProvisioning
		case "running", "paused":
			state = runtimepkg.StateRunning
		case "restarting":
			state = runtimepkg.StateStarting
		case "removing":
			state = runtimepkg.StateStopping
		case "exited":
			state = runtimepkg.StateStopped
		case "dead":
			state = runtimepkg.StateFailed
		}
	}
	return runtimepkg.Inspection{ExternalID: inspected.ID, State: state}
}

func containerName(runtimeInstanceID string) string {
	return containerNamePrefix + runtimeInstanceID
}
