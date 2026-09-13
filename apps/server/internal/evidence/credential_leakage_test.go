package evidence

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type credentialLeakCaptureStore struct {
	captureStore
	transition store.SchedulerTransition
}

func (s *credentialLeakCaptureStore) TransitionAdmittedJob(_ context.Context, input store.SchedulerTransition) (store.Run, error) {
	s.transition = input
	return store.Run{}, nil
}

func TestCredentialSentinelsAreAbsentFromPersistedEvidenceAndFailures(t *testing.T) {
	const runID = "credential-leak-run"
	secrets := []string{
		"PASSWORD-SENTINEL-79",
		"ACCESS-TOKEN-SENTINEL-79",
		"REFRESH-TOKEN-SENTINEL-79",
		"PROVIDER-CREDENTIAL-SENTINEL-79",
	}
	registry := redaction.NewRegistry()
	registry.Register(runID, secrets)
	base := &credentialLeakCaptureStore{}
	secured := NewRedactingStore(base, registry)
	joined := strings.Join(secrets, " | ")

	if err := secured.PutRunProvenance(t.Context(), "project", runID, json.RawMessage(`{"credentials":"`+joined+`"}`)); err != nil {
		t.Fatal(err)
	}
	runRef := runID
	if _, err := secured.AppendEvent(t.Context(), store.Event{
		RunID: &runRef, Actor: json.RawMessage(`{"identity":"`+joined+`"}`), Payload: json.RawMessage(`{"message":"`+joined+`"}`),
	}); err != nil {
		t.Fatal(err)
	}
	digest := joined
	if _, err := secured.CreateRawOutputChunk(t.Context(), store.RawOutputChunk{RunID: runID, StorageRef: joined, Digest: &digest}); err != nil {
		t.Fatal(err)
	}
	mediaType := joined
	if _, err := secured.CreateArtifact(t.Context(), store.Artifact{RunID: runID, Name: joined, Kind: joined, MediaType: &mediaType, StorageRef: joined, SafeMetadata: json.RawMessage(`{"value":"`+joined+`"}`)}); err != nil {
		t.Fatal(err)
	}
	reason := "execution failed: " + joined
	if _, err := secured.TransitionAdmittedJob(t.Context(), store.SchedulerTransition{
		ProjectID: "project", JobID: "job", RunID: runID, LeaseToken: "lease", RunStatus: "FAILED", FailureReason: &reason,
	}); err != nil {
		t.Fatal(err)
	}

	persisted := []string{
		string(base.provenance),
		string(base.event.Actor),
		string(base.event.Payload),
		base.raw.StorageRef,
		valueOrEmpty(base.raw.Digest),
		base.artifact.Name,
		base.artifact.Kind,
		valueOrEmpty(base.artifact.MediaType),
		base.artifact.StorageRef,
		string(base.artifact.SafeMetadata),
		valueOrEmpty(base.transition.FailureReason),
	}
	for _, secret := range secrets {
		for _, value := range persisted {
			if strings.Contains(value, secret) {
				t.Fatalf("persisted evidence leaked %q in %q", secret, value)
			}
		}
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
