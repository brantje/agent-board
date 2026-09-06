package evidence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runtimeRunnerStatusStore interface {
	UpdateRuntimeInstanceRunnerStatusIfStatus(context.Context, string, string, string, string) (store.RuntimeInstance, error)
}

type runtimeRunnerGenerationStore interface {
	ClaimRuntimeInstanceRunnerGeneration(context.Context, string, string) (int64, error)
	UpdateRuntimeInstanceRunnerStatusGeneration(context.Context, string, string, string, int64) (store.RuntimeInstance, error)
	UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(context.Context, string, string, string, int64, string) (store.RuntimeInstance, error)
}

// RedactingStore keeps the full control-plane store contract while overriding
// every current durable evidence write with a final server-side sanitizer.
type RedactingStore struct {
	store.ControlPlaneStore
	registry *redaction.Registry
}

func NewRedactingStore(base store.ControlPlaneStore, registry *redaction.Registry) *RedactingStore {
	return &RedactingStore{ControlPlaneStore: base, registry: registry}
}

func (s *RedactingStore) PutRunProvenance(ctx context.Context, projectID, runID string, snapshot json.RawMessage) error {
	redacted, err := s.registry.RedactJSON(runID, snapshot)
	if err != nil {
		return err
	}
	return s.ControlPlaneStore.PutRunProvenance(ctx, projectID, runID, redacted)
}

func (s *RedactingStore) AppendEvent(ctx context.Context, input store.Event) (store.Event, error) {
	var err error
	if input.RunID == nil {
		input.Actor, err = s.registry.RedactAllJSON(input.Actor)
	} else {
		input.Actor, err = s.registry.RedactJSON(*input.RunID, input.Actor)
	}
	if err != nil {
		return store.Event{}, err
	}
	if input.RunID == nil {
		input.Payload, err = s.registry.RedactAllJSON(input.Payload)
	} else {
		input.Payload, err = s.registry.RedactJSON(*input.RunID, input.Payload)
	}
	if err != nil {
		return store.Event{}, err
	}
	return s.ControlPlaneStore.AppendEvent(ctx, input)
}

func (s *RedactingStore) CreateRawOutputChunk(ctx context.Context, input store.RawOutputChunk) (store.RawOutputChunk, error) {
	input.StorageRef = s.registry.RedactString(input.RunID, input.StorageRef)
	if input.Digest != nil {
		value := s.registry.RedactString(input.RunID, *input.Digest)
		input.Digest = &value
	}
	return s.ControlPlaneStore.CreateRawOutputChunk(ctx, input)
}

func (s *RedactingStore) CreateArtifact(ctx context.Context, input store.Artifact) (store.Artifact, error) {
	input.Name = s.registry.RedactString(input.RunID, input.Name)
	input.Kind = s.registry.RedactString(input.RunID, input.Kind)
	input.StorageRef = s.registry.RedactString(input.RunID, input.StorageRef)
	if input.MediaType != nil {
		value := s.registry.RedactString(input.RunID, *input.MediaType)
		input.MediaType = &value
	}
	if input.Digest != nil {
		value := s.registry.RedactString(input.RunID, *input.Digest)
		input.Digest = &value
	}
	redacted, err := s.registry.RedactJSON(input.RunID, input.SafeMetadata)
	if err != nil {
		return store.Artifact{}, err
	}
	input.SafeMetadata = redacted
	return s.ControlPlaneStore.CreateArtifact(ctx, input)
}

// The runner ownership methods are intentionally not part of ControlPlaneStore:
// they are optional capabilities used by RuntimeInstanceService for connection
// fencing. Explicit forwarding keeps those capabilities visible through this
// decorator without widening the store contract for unrelated implementations.
func (s *RedactingStore) UpdateRuntimeInstanceRunnerStatusIfStatus(ctx context.Context, projectID, instanceID, status, expectedStatus string) (store.RuntimeInstance, error) {
	base, ok := s.ControlPlaneStore.(runtimeRunnerStatusStore)
	if !ok {
		return store.RuntimeInstance{}, fmt.Errorf("redacting store base does not support lifecycle-fenced runner status updates")
	}
	return base.UpdateRuntimeInstanceRunnerStatusIfStatus(ctx, projectID, instanceID, status, expectedStatus)
}

func (s *RedactingStore) ClaimRuntimeInstanceRunnerGeneration(ctx context.Context, projectID, instanceID string) (int64, error) {
	base, ok := s.ControlPlaneStore.(runtimeRunnerGenerationStore)
	if !ok {
		return 0, fmt.Errorf("redacting store base does not support runner connection generations")
	}
	return base.ClaimRuntimeInstanceRunnerGeneration(ctx, projectID, instanceID)
}

func (s *RedactingStore) UpdateRuntimeInstanceRunnerStatusGeneration(ctx context.Context, projectID, instanceID, status string, generation int64) (store.RuntimeInstance, error) {
	base, ok := s.ControlPlaneStore.(runtimeRunnerGenerationStore)
	if !ok {
		return store.RuntimeInstance{}, fmt.Errorf("redacting store base does not support runner connection generations")
	}
	return base.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, projectID, instanceID, status, generation)
}

func (s *RedactingStore) UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(ctx context.Context, projectID, instanceID, status string, generation int64, expectedStatus string) (store.RuntimeInstance, error) {
	base, ok := s.ControlPlaneStore.(runtimeRunnerGenerationStore)
	if !ok {
		return store.RuntimeInstance{}, fmt.Errorf("redacting store base does not support runner connection generations")
	}
	return base.UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(ctx, projectID, instanceID, status, generation, expectedStatus)
}
