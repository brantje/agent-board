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
func (s *captureStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) {
	return nil, store.ErrNotFound
}
func (s *captureStore) AppendEvent(_ context.Context, input store.Event) (store.Event, error) {
	s.event = input
	return input, nil
}
func (s *captureStore) ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error) {
	return nil, nil
}
func (s *captureStore) CreateRawOutputChunk(_ context.Context, input store.RawOutputChunk) (store.RawOutputChunk, error) {
	s.raw = input
	return input, nil
}
func (s *captureStore) CreateArtifact(_ context.Context, input store.Artifact) (store.Artifact, error) {
	s.artifact = input
	return input, nil
}
func (s *captureStore) ListArtifacts(context.Context, string, string) ([]store.Artifact, error) {
	return nil, nil
}

func TestRedactingStoreSanitizesEveryEvidenceWrite(t *testing.T) {
	registry := redaction.NewRegistry()
	secret := "canary-secret-value"
	registry.Register("run-1", []string{secret})
	base := &captureStore{}
	s := NewRedactingStore(base, registry)
	ctx := context.Background()

	if err := s.PutRunProvenance(ctx, "project-1", "run-1", json.RawMessage(`{"value":"canary-secret-value"}`)); err != nil {
		t.Fatal(err)
	}
	runID := "run-1"
	if _, err := s.AppendEvent(ctx, store.Event{RunID: &runID, Actor: json.RawMessage(`{"name":"canary-secret-value"}`), Payload: json.RawMessage(`{"message":"x-canary-secret-value-y"}`)}); err != nil {
		t.Fatal(err)
	}
	digest := secret
	if _, err := s.CreateRawOutputChunk(ctx, store.RawOutputChunk{RunID: "run-1", StorageRef: "blob/" + secret, Digest: &digest}); err != nil {
		t.Fatal(err)
	}
	media := "text/" + secret
	if _, err := s.CreateArtifact(ctx, store.Artifact{RunID: "run-1", Name: "artifact-" + secret, Kind: secret, MediaType: &media, StorageRef: "blob/" + secret, SafeMetadata: json.RawMessage(`{"secret":"canary-secret-value"}`)}); err != nil {
		t.Fatal(err)
	}

	for name, value := range map[string]string{
		"provenance":        string(base.provenance),
		"event actor":       string(base.event.Actor),
		"event payload":     string(base.event.Payload),
		"raw storage":       base.raw.StorageRef,
		"raw digest":        *base.raw.Digest,
		"artifact name":     base.artifact.Name,
		"artifact kind":     base.artifact.Kind,
		"artifact media":    *base.artifact.MediaType,
		"artifact storage":  base.artifact.StorageRef,
		"artifact metadata": string(base.artifact.SafeMetadata),
	} {
		if strings.Contains(value, secret) {
			t.Errorf("%s leaked secret: %q", name, value)
		}
	}
}

type runnerCapabilityStore struct {
	store.ControlPlaneStore
	instance   store.RuntimeInstance
	generation int64
}

func (s *runnerCapabilityStore) UpdateRuntimeInstanceRunnerStatusIfStatus(_ context.Context, _, _ string, status, _ string) (store.RuntimeInstance, error) {
	s.instance.RunnerStatus = status
	return s.instance, nil
}
func (s *runnerCapabilityStore) ClaimRuntimeInstanceRunnerGeneration(context.Context, string, string) (int64, error) {
	s.generation++
	return s.generation, nil
}
func (s *runnerCapabilityStore) UpdateRuntimeInstanceRunnerStatusGeneration(_ context.Context, _, _ string, status string, generation int64) (store.RuntimeInstance, error) {
	s.generation = generation
	s.instance.RunnerStatus = status
	return s.instance, nil
}
func (s *runnerCapabilityStore) UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(_ context.Context, _, _ string, status string, generation int64, _ string) (store.RuntimeInstance, error) {
	s.generation = generation
	s.instance.RunnerStatus = status
	return s.instance, nil
}

func TestRedactingStorePreservesRunnerFencingCapabilities(t *testing.T) {
	base := &runnerCapabilityStore{instance: store.RuntimeInstance{ID: "runtime", Status: "RUNNING"}}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())
	ctx := t.Context()

	generation, err := wrapped.ClaimRuntimeInstanceRunnerGeneration(ctx, "project", "runtime")
	if err != nil || generation != 1 {
		t.Fatalf("claim generation=%d err=%v", generation, err)
	}
	if got, err := wrapped.UpdateRuntimeInstanceRunnerStatusIfStatus(ctx, "project", "runtime", "READY", "RUNNING"); err != nil || got.RunnerStatus != "READY" {
		t.Fatalf("lifecycle-fenced status=%+v err=%v", got, err)
	}
	if got, err := wrapped.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, "project", "runtime", "BUSY", generation); err != nil || got.RunnerStatus != "BUSY" {
		t.Fatalf("generation status=%+v err=%v", got, err)
	}
	if got, err := wrapped.UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(ctx, "project", "runtime", "UNAVAILABLE", generation, "RUNNING"); err != nil || got.RunnerStatus != "UNAVAILABLE" {
		t.Fatalf("generation/lifecycle status=%+v err=%v", got, err)
	}
}

func TestRedactingStoreReportsMissingRunnerFencingCapabilities(t *testing.T) {
	wrapped := NewRedactingStore(&captureStore{}, redaction.NewRegistry())
	ctx := t.Context()
	if _, err := wrapped.ClaimRuntimeInstanceRunnerGeneration(ctx, "project", "runtime"); err == nil {
		t.Fatal("expected missing generation capability error")
	}
	if _, err := wrapped.UpdateRuntimeInstanceRunnerStatusIfStatus(ctx, "project", "runtime", "READY", "RUNNING"); err == nil {
		t.Fatal("expected missing lifecycle-fenced status capability error")
	}
	if _, err := wrapped.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, "project", "runtime", "READY", 1); err == nil {
		t.Fatal("expected missing generation status capability error")
	}
	if _, err := wrapped.UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(ctx, "project", "runtime", "READY", 1, "RUNNING"); err == nil {
		t.Fatal("expected missing generation/lifecycle status capability error")
	}
}
