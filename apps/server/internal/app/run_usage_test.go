package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/modelusage"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunUsageReconstructsPersistedUsageAndIgnoresMalformedEvents(t *testing.T) {
	limit := int64(200000)
	base := time.Unix(100, 0).UTC()
	start := base
	first := base.Add(time.Second)
	complete := base.Add(2 * time.Second)
	firstSample := modelusage.Sample{
		SampleID: "step-1", ProviderID: "openrouter", ModelID: "model",
		InputTokens: 100, OutputTokens: 20, CacheReadTokens: 40, ContextTokens: 160,
		ContextLimitTokens: &limit, StartedAt: &start, FirstOutputAt: &first, CompletedAt: &complete,
	}
	secondSample := modelusage.Sample{
		SampleID: "step-2", ProviderID: "openrouter", ModelID: "model",
		InputTokens: 200, OutputTokens: 30, CacheReadTokens: 50, ContextTokens: 280,
		ContextLimitTokens: &limit,
	}
	storeFake := &runEvidenceTestStore{
		run:       store.Run{ID: "run", ProjectID: "project"},
		instances: map[string]store.RuntimeInstance{},
		events: []store.Event{
			usageEvent(t, 1, firstSample),
			{ID: "bad", ProjectID: "project", RunID: runEvidenceStringPointer("run"), Sequence: runUsageSequence(2), Type: "model.usage", Payload: json.RawMessage(`{"sampleId":`)},
			usageEvent(t, 3, firstSample),
			usageEvent(t, 4, secondSample),
		},
	}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}

	usage, err := service.Usage(t.Context(), "project", "run")
	if err != nil {
		t.Fatal(err)
	}
	if usage == nil || usage.InputTokens != 300 || usage.OutputTokens != 50 || usage.CacheReadTokens != 90 || usage.ContextTokens != 280 {
		t.Fatalf("usage=%+v", usage)
	}
	if usage.ContextLimitTokens == nil || *usage.ContextLimitTokens != limit {
		t.Fatalf("context limit=%v", usage.ContextLimitTokens)
	}

	inspected, err := service.Inspect(t.Context(), "project", "run")
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(inspected.Usage)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("inspect usage=%s direct=%s", gotJSON, wantJSON)
	}
}

func TestRunUsageIsNilWithoutValidTelemetry(t *testing.T) {
	storeFake := &runEvidenceTestStore{run: store.Run{ID: "run", ProjectID: "project"}, instances: map[string]store.RuntimeInstance{}}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := service.Usage(t.Context(), "project", "run")
	if err != nil {
		t.Fatal(err)
	}
	if usage != nil {
		t.Fatalf("usage=%+v, want nil", usage)
	}
}

func usageEvent(t *testing.T, sequence int64, sample modelusage.Sample) store.Event {
	t.Helper()
	payload, err := evidence.EncodePayload(sample)
	if err != nil {
		t.Fatal(err)
	}
	return store.Event{
		ID: "usage", ProjectID: "project", RunID: runEvidenceStringPointer("run"), Sequence: runUsageSequence(sequence), Type: "model.usage", Payload: payload,
	}
}

func runUsageSequence(value int64) *int64 { return &value }
