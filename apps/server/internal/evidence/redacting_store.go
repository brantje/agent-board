package evidence

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

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
