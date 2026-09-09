package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/modelusage"
)

type recordingUsageSink struct {
	samples []modelusage.Sample
}

func (*recordingUsageSink) RecordActivity(context.Context, engine.ActivityEvent) error { return nil }
func (s *recordingUsageSink) RecordModelUsage(_ context.Context, sample modelusage.Sample) error {
	s.samples = append(s.samples, sample)
	return nil
}

func TestUsageTelemetryCapturesStepTimingTokensAndDeduplicates(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	limit := int64(200000)
	state.providerID = "openrouter"
	state.modelID = "model"
	state.contextLimitTokens = &limit

	updates := []map[string]any{
		{"sessionID": "ses_1", "time": 1000, "part": map[string]any{"id": "start_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-start"}},
		{"sessionID": "ses_1", "time": 2500, "part": map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "reasoning", "text": "native visible reasoning"}},
		{"sessionID": "ses_1", "time": 4500, "part": map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 100, "output": 20, "reasoning": 10, "cache": map[string]any{"read": 40, "write": 5}}}},
		{"sessionID": "ses_1", "time": 4500, "part": map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 100, "output": 20, "reasoning": 10, "cache": map[string]any{"read": 40, "write": 5}}}},
	}
	for _, update := range updates {
		if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: mustJSON(t, update)}); err != nil {
			t.Fatalf("handleEvent() error=%v", err)
		}
	}

	if len(sink.samples) != 1 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	sample := sink.samples[0]
	if sample.SampleID != "opencode:ses_1:finish_1" || sample.ProviderID != "openrouter" || sample.ModelID != "model" {
		t.Fatalf("identity=%+v", sample)
	}
	if sample.InputTokens != 100 || sample.OutputTokens != 20 || sample.ReasoningTokens != 10 || sample.CacheReadTokens != 40 || sample.CacheWriteTokens != 5 || sample.ContextTokens != 175 {
		t.Fatalf("tokens=%+v", sample)
	}
	if sample.ContextLimitTokens == nil || *sample.ContextLimitTokens != limit {
		t.Fatalf("context limit=%v", sample.ContextLimitTokens)
	}
	assertTimeMillis(t, sample.StartedAt, 1000)
	assertTimeMillis(t, sample.FirstOutputAt, 2500)
	assertTimeMillis(t, sample.CompletedAt, 4500)
}

func TestUsageTelemetryTreatsToolCallAsFirstGeneratedOutput(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.providerID = "openrouter"
	state.modelID = "model"

	for _, update := range []map[string]any{
		{"sessionID": "ses_1", "time": 1000, "part": map[string]any{"id": "start_1", "messageID": "msg_1", "type": "step-start"}},
		{"sessionID": "ses_1", "time": 1600, "part": map[string]any{"id": "tool_1", "messageID": "msg_1", "type": "tool", "tool": "bash", "state": map[string]any{"status": "pending"}}},
		{"sessionID": "ses_1", "time": 2600, "part": map[string]any{"id": "finish_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 10, "output": 5, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}}},
	} {
		if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: mustJSON(t, update)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.samples) != 1 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	assertTimeMillis(t, sink.samples[0].FirstOutputAt, 1600)
}

func TestUsageMessageMetadataResolvesContextLimitBestEffort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"all":[{"id":"openrouter","models":{"model":{"limit":{"context":128000}}}}]}`))
	}))
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)

	message := mustJSON(t, map[string]any{
		"sessionID": "ses_1",
		"info":      map[string]any{"sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model"},
	})
	if err := state.handleEvent(t.Context(), native, client.Event{Type: "message.updated", Properties: message}); err != nil {
		t.Fatal(err)
	}
	if state.providerID != "openrouter" || state.modelID != "model" || state.contextLimitTokens == nil || *state.contextLimitTokens != 128000 {
		t.Fatalf("selection provider=%q model=%q limit=%v", state.providerID, state.modelID, state.contextLimitTokens)
	}

	finish := mustJSON(t, map[string]any{
		"sessionID": "ses_1", "time": 3000,
		"part": map[string]any{"id": "finish_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 12, "output": 3, "reasoning": 0, "cache": map[string]any{"read": 2, "write": 0}}},
	})
	if err := state.handleEvent(t.Context(), native, client.Event{Type: "message.part.updated", Properties: finish}); err != nil {
		t.Fatal(err)
	}
	if len(sink.samples) != 1 || sink.samples[0].ContextLimitTokens == nil || *sink.samples[0].ContextLimitTokens != 128000 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	if sink.samples[0].StartedAt != nil || sink.samples[0].FirstOutputAt != nil {
		t.Fatalf("missing step timing was manufactured: %+v", sink.samples[0])
	}
}

func TestUsageTelemetryIgnoresForeignSession(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.providerID = "p"
	state.modelID = "m"
	finish := mustJSON(t, map[string]any{
		"sessionID": "other", "time": 1000,
		"part": map[string]any{"id": "finish", "type": "step-finish", "tokens": map[string]any{"input": 1, "output": 1, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}},
	})
	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: finish}); err != nil {
		t.Fatal(err)
	}
	if len(sink.samples) != 0 {
		t.Fatalf("foreign session samples=%+v", sink.samples)
	}
}

func assertTimeMillis(t *testing.T, got *time.Time, want int64) {
	t.Helper()
	if got == nil || got.UnixMilli() != want {
		t.Fatalf("time=%v want=%dms", got, want)
	}
}

var _ engine.ActivitySink = (*recordingUsageSink)(nil)
var _ engine.UsageSink = (*recordingUsageSink)(nil)
