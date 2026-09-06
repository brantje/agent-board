package evidence

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type captureStore struct {
	store.ControlPlaneStore
	provenance json.RawMessage
	event      store.Event
	artifact   store.Artifact
	raw        store.RawOutputChunk
}

func (s *captureStore) PutRunProvenance(_ context.Context, _, _ string, snapshot json.RawMessage) error {
	s.provenance = append(json.RawMessage(nil), snapshot...)
	return nil
}
func (s *captureStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) { return nil, store.ErrNotFound }
func (s *captureStore) AppendEvent(_ context.Context, input store.Event) (store.Event, error) { s.event = input; return input, nil }
func (s *captureStore) ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error) { return nil, nil }
func (s *captureStore) CreateRawOutputChunk(_ context.Context, input store.RawOutputChunk) (store.RawOutputChunk, error) { s.raw = input; return input, nil }
func (s *captureStore) CreateArtifact(_ context.Context, input store.Artifact) (store.Artifact, error) { s.artifact = input; return input, nil }
func (s *captureStore) ListArtifacts(context.Context, string, string) ([]store.Artifact, error) { return nil, nil }

func TestRedactingStoreSanitizesEveryEvidenceWrite(t *testing.T) {
	registry := redaction.NewRegistry()
	secret := "canary-secret-value"
	registry.Register("run-1", []string{secret})
	base := &captureStore{}
	s := NewRedactingStore(base, registry)
	ctx := context.Background()

	if err := s.PutRunProvenance(ctx, "project-1", "run-1", json.RawMessage(`{"value":"canary-secret-value"}`)); err != nil { t.Fatal(err) }
	runID := "run-1"
	if _, err := s.AppendEvent(ctx, store.Event{RunID: &runID, Actor: json.RawMessage(`{"name":"canary-secret-value"}`), Payload: json.RawMessage(`{"message":"x-canary-secret-value-y"}`)}); err != nil { t.Fatal(err) }
	digest := secret
	if _, err := s.CreateRawOutputChunk(ctx, store.RawOutputChunk{RunID: "run-1", StorageRef: "blob/" + secret, Digest: &digest}); err != nil { t.Fatal(err) }
	media := "text/" + secret
	if _, err := s.CreateArtifact(ctx, store.Artifact{RunID: "run-1", Name: "artifact-" + secret, Kind: secret, MediaType: &media, StorageRef: "blob/" + secret, SafeMetadata: json.RawMessage(`{"secret":"canary-secret-value"}`)}); err != nil { t.Fatal(err) }

	for name, value := range map[string]string{
		"provenance": string(base.provenance),
		"event actor": string(base.event.Actor),
		"event payload": string(base.event.Payload),
		"raw storage": base.raw.StorageRef,
		"raw digest": *base.raw.Digest,
		"artifact name": base.artifact.Name,
		"artifact kind": base.artifact.Kind,
		"artifact media": *base.artifact.MediaType,
		"artifact storage": base.artifact.StorageRef,
		"artifact metadata": string(base.artifact.SafeMetadata),
	} {
		if strings.Contains(value, secret) {
			t.Errorf("%s leaked secret: %q", name, value)
		}
	}
}
