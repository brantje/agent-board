package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

const WorkspaceTarget = "/workspace"

type State string

const (
	StateProvisioning State = "PROVISIONING"
	StateStarting     State = "STARTING"
	StateRunning      State = "RUNNING"
	StateStopping     State = "STOPPING"
	StateFailed       State = "FAILED"
	StateStopped      State = "STOPPED"
	StateDestroyed    State = "DESTROYED"
)

type NetworkPolicy string

const (
	NetworkNone       NetworkPolicy = "none"
	NetworkRestricted NetworkPolicy = "restricted"
	NetworkOutbound   NetworkPolicy = "outbound"
)

type ResourcePolicy struct {
	CPULimitMillis   *int
	MemoryLimitBytes *int64
	PIDLimit         *int
	TimeoutSeconds   *int
}

type WorkspaceMount struct {
	WorkspaceID string
	Source      string
	Target      string
}

type RuntimeSpec struct {
	RuntimeInstanceID string
	ProjectID         string
	IssueID           string
	WorkspaceID       string
	RuntimeID         string
	Image             string
	WorkingDirectory  string
	Resources         ResourcePolicy
	Workspace         WorkspaceMount
	Network           NetworkPolicy
	AllowedSecretRefs []string
	Labels            map[string]string
}

type Handle struct {
	ExternalID string
	Metadata   json.RawMessage
}

type Inspection struct {
	ExternalID string
	State      State
}

type StopReason string

const (
	StopReasonRequested StopReason = "requested"
	StopReasonShutdown  StopReason = "shutdown"
	StopReasonFailed    StopReason = "failed"
)

type Implementation interface {
	Create(context.Context, RuntimeSpec) (Handle, error)
	Start(context.Context, Handle) error
	Inspect(context.Context, Handle) (Inspection, error)
	Stop(context.Context, Handle, StopReason) error
	Destroy(context.Context, Handle) error
}

var (
	ErrInvalidSpec       = errors.New("runtime: invalid spec")
	ErrInvalidTransition = errors.New("runtime: invalid lifecycle transition")
	ErrNotFound          = errors.New("runtime: external instance not found")
	ErrOwnershipMismatch = errors.New("runtime: external instance ownership mismatch")
	ErrUnsupportedPolicy = errors.New("runtime: unsupported policy")
)

func ValidateSpec(spec RuntimeSpec) error {
	required := map[string]string{
		"runtimeInstanceID": spec.RuntimeInstanceID,
		"projectID":         spec.ProjectID,
		"issueID":           spec.IssueID,
		"workspaceID":       spec.WorkspaceID,
		"runtimeID":         spec.RuntimeID,
		"image":             spec.Image,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidSpec, field)
		}
	}
	if strings.TrimSpace(spec.Workspace.WorkspaceID) == "" || spec.Workspace.WorkspaceID != spec.WorkspaceID {
		return fmt.Errorf("%w: workspace mount must match workspaceID", ErrInvalidSpec)
	}
	if strings.TrimSpace(spec.Workspace.Source) == "" {
		return fmt.Errorf("%w: workspace source is required", ErrInvalidSpec)
	}
	if spec.Workspace.Target != WorkspaceTarget {
		return fmt.Errorf("%w: workspace target must be %s", ErrInvalidSpec, WorkspaceTarget)
	}
	if spec.WorkingDirectory == "" {
		return fmt.Errorf("%w: working directory is required", ErrInvalidSpec)
	}
	cleanDir := path.Clean(spec.WorkingDirectory)
	if cleanDir != WorkspaceTarget && !strings.HasPrefix(cleanDir, WorkspaceTarget+"/") {
		return fmt.Errorf("%w: working directory must stay within %s", ErrInvalidSpec, WorkspaceTarget)
	}
	if err := validateResources(spec.Resources); err != nil {
		return err
	}
	switch spec.Network {
	case NetworkNone, NetworkRestricted, NetworkOutbound:
	default:
		return fmt.Errorf("%w: unknown network policy %q", ErrInvalidSpec, spec.Network)
	}
	for key, value := range spec.Labels {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: labels must have non-empty keys and values", ErrInvalidSpec)
		}
	}
	return nil
}

func validateResources(resources ResourcePolicy) error {
	if resources.CPULimitMillis != nil && *resources.CPULimitMillis < 1 {
		return fmt.Errorf("%w: cpu limit must be positive", ErrInvalidSpec)
	}
	if resources.MemoryLimitBytes != nil && *resources.MemoryLimitBytes < 1 {
		return fmt.Errorf("%w: memory limit must be positive", ErrInvalidSpec)
	}
	if resources.PIDLimit != nil && *resources.PIDLimit < 1 {
		return fmt.Errorf("%w: pid limit must be positive", ErrInvalidSpec)
	}
	if resources.TimeoutSeconds != nil && *resources.TimeoutSeconds < 1 {
		return fmt.Errorf("%w: timeout must be positive", ErrInvalidSpec)
	}
	return nil
}

func ValidateTransition(from, to State) error {
	if from == to {
		return nil
	}
	allowed := map[State]map[State]bool{
		StateProvisioning: {StateStarting: true, StateFailed: true, StateDestroyed: true},
		StateStarting:     {StateRunning: true, StateFailed: true, StateStopping: true},
		StateRunning:      {StateStopping: true, StateFailed: true},
		StateStopping:     {StateStopped: true, StateFailed: true},
		StateFailed:       {StateStopping: true, StateStopped: true, StateDestroyed: true},
		StateStopped:      {StateStarting: true, StateDestroyed: true},
		StateDestroyed:    {},
	}
	if allowed[from][to] {
		return nil
	}
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
}
