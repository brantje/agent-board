package runexec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/modelusage"
)

func TestProcessLauncherRecordModelUsagePersistsRunScopedEvent(t *testing.T) {
	events := &questionEventStore{}
	recorder, err := evidence.NewRecorder(events, nil)
	if err != nil {
		t.Fatal(err)
	}
	launcher := &processLauncher{
		events:            recorder,
		safe:              interactiveSafeContext(),
		runtimeInstanceID: "runtime-instance-1",
	}
	limit := int64(200000)
	sample := modelusage.Sample{
		SampleID:           "opencode:session:part",
		ProviderID:         "openrouter",
		ModelID:            "model",
		InputTokens:        100,
		OutputTokens:       20,
		CacheReadTokens:    40,
		ContextTokens:      160,
		ContextLimitTokens: &limit,
	}
	if err := launcher.RecordModelUsage(context.Background(), sample); err != nil {
		t.Fatalf("RecordModelUsage() error=%v", err)
	}
	if len(events.events) != 1 || events.events[0].Type != "model.usage" || events.events[0].RunID == nil || *events.events[0].RunID != "run-1" {
		t.Fatalf("events=%+v", events.events)
	}
	if events.events[0].RuntimeInstanceID == nil || *events.events[0].RuntimeInstanceID != "runtime-instance-1" {
		t.Fatalf("runtime instance=%v", events.events[0].RuntimeInstanceID)
	}
	var recorded modelusage.Sample
	if err := json.Unmarshal(events.events[0].Payload, &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.SampleID != sample.SampleID || recorded.InputTokens != sample.InputTokens || recorded.ContextLimitTokens == nil || *recorded.ContextLimitTokens != limit {
		t.Fatalf("recorded=%+v", recorded)
	}

	if err := launcher.RecordModelUsage(context.Background(), modelusage.Sample{}); err == nil {
		t.Fatal("invalid usage unexpectedly persisted")
	}
	if len(events.events) != 1 {
		t.Fatalf("events after invalid usage=%+v", events.events)
	}
	var unavailable *processLauncher
	if err := unavailable.RecordModelUsage(context.Background(), sample); err == nil {
		t.Fatal("nil usage sink unexpectedly accepted sample")
	}
}
