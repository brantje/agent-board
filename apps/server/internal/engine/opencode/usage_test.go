package opencode

import (
	"context"
	"encoding/json"
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

	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.updated", Properties: mustJSON(t, map[string]any{
		"info": map[string]any{
			"id": "msg_1", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
			"time": map[string]any{"created": 1000},
		},
	})}); err != nil {
		t.Fatal(err)
	}
	state.contextLimitTokens = &limit

	updates := []map[string]any{
		{"sessionID": "ses_1", "part": map[string]any{"id": "start_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-start"}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "reasoning", "text": "native visible reasoning", "time": map[string]any{"start": 2500}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "reasoning", "text": "native visible reasoning", "time": map[string]any{"start": 2500, "end": 4500}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 100, "output": 20, "reasoning": 10, "cache": map[string]any{"read": 40, "write": 5}}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 100, "output": 20, "reasoning": 10, "cache": map[string]any{"read": 40, "write": 5}}}},
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

func TestUsageTelemetryRecordsFirstStepFromSeededSelection(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.seedModelUsage("openrouter", "model", nil)
	state.now = func() time.Time { return time.UnixMilli(4000).UTC() }

	for _, update := range []map[string]any{
		{"part": map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "reasoning", "text": "", "time": map[string]any{"start": 2500}}},
		{"part": map[string]any{"id": "start_extra", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-start"}},
		{"part": map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 8051, "output": 20, "reasoning": 77, "cache": map[string]any{"read": 0, "write": 0}}}},
	} {
		if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: mustJSON(t, update)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.samples) != 1 {
		t.Fatalf("first native step was dropped: samples=%+v", sink.samples)
	}
	assertTimeMillis(t, sink.samples[0].FirstOutputAt, 2500)
	assertTimeMillis(t, sink.samples[0].CompletedAt, 4000)
}

func TestUsageTelemetryKeepsTimesAcrossRepeatedStepStarts(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.seedModelUsage("openrouter", "model", nil)

	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.updated", Properties: mustJSON(t, map[string]any{
		"info": map[string]any{
			"id": "msg_1", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
			"time": map[string]any{"created": 1000, "completed": 4500},
		},
	})}); err != nil {
		t.Fatal(err)
	}

	for _, update := range []map[string]any{
		{"sessionID": "ses_1", "part": map[string]any{"id": "start_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-start"}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "reasoning", "text": "plan", "time": map[string]any{"start": 1200, "end": 3000}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "start_2", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-start"}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 10, "output": 8, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}}},
	} {
		if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: mustJSON(t, update)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.samples) != 1 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	assertTimeMillis(t, sink.samples[0].StartedAt, 1000)
	assertTimeMillis(t, sink.samples[0].FirstOutputAt, 1200)
	assertTimeMillis(t, sink.samples[0].CompletedAt, 3000)
}

func TestUsageTelemetryTreatsToolCallAsFirstGeneratedOutput(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.providerID = "openrouter"
	state.modelID = "model"
	state.now = func() time.Time { return time.UnixMilli(1800).UTC() }

	for _, update := range []map[string]any{
		{"sessionID": "ses_1", "part": map[string]any{"id": "start_1", "messageID": "msg_1", "type": "step-start"}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "tool_1", "messageID": "msg_1", "type": "tool", "tool": "bash", "state": map[string]any{"status": "running", "time": map[string]any{"start": 1600}}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "finish_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 10, "output": 5, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}}},
	} {
		if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: mustJSON(t, update)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.samples) != 1 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	assertTimeMillis(t, sink.samples[0].FirstOutputAt, 1600)
	assertTimeMillis(t, sink.samples[0].CompletedAt, 1800)
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
		"sessionID": "ses_1",
		"part":      map[string]any{"id": "finish_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 12, "output": 3, "reasoning": 0, "cache": map[string]any{"read": 2, "write": 0}}},
	})
	if err := state.handleEvent(t.Context(), native, client.Event{Type: "message.part.updated", Properties: finish}); err != nil {
		t.Fatal(err)
	}
	if len(sink.samples) != 1 || sink.samples[0].ContextLimitTokens == nil || *sink.samples[0].ContextLimitTokens != 128000 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	if sink.samples[0].StartedAt != nil || sink.samples[0].FirstOutputAt != nil || sink.samples[0].CompletedAt != nil {
		t.Fatalf("missing step timing was manufactured: %+v", sink.samples[0])
	}
}

func TestUsageBackfillRecordsCompletedHistoryAndSkipsInFlightSteps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/ses_1/message" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{
				"info": map[string]any{
					"id": "msg_done", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
					"time": map[string]any{"created": 1000, "completed": 4000},
				},
				"parts": []any{
					map[string]any{"id": "start_1", "sessionID": "ses_1", "messageID": "msg_done", "type": "step-start"},
					map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_done", "type": "reasoning", "text": "plan", "time": map[string]any{"start": 1500, "end": 3500}},
					map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_done", "type": "step-finish", "tokens": map[string]any{"input": 8054, "output": 269, "reasoning": 90, "cache": map[string]any{"read": 0, "write": 0}}},
				},
			},
			map[string]any{
				"info": map[string]any{
					"id": "msg_open", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
					"time":   map[string]any{"created": 5000},
					"tokens": map[string]any{"input": 0, "output": 0, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
				},
				"parts": []any{
					map[string]any{"id": "start_2", "sessionID": "ses_1", "messageID": "msg_open", "type": "step-start"},
					map[string]any{"id": "reason_2", "sessionID": "ses_1", "messageID": "msg_open", "type": "reasoning", "text": "", "time": map[string]any{"start": 5100}},
					map[string]any{"id": "tool_q", "sessionID": "ses_1", "messageID": "msg_open", "type": "tool", "tool": "question", "state": map[string]any{"status": "running"}},
				},
			},
		})
	}))
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}

	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.seedModelUsage("openrouter", "model", nil)
	if err := state.backfillUsageFromHistory(t.Context(), native); err != nil {
		t.Fatal(err)
	}
	if len(sink.samples) != 1 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	if sink.samples[0].SampleID != "opencode:ses_1:finish_1" || sink.samples[0].InputTokens != 8054 || sink.samples[0].OutputTokens != 269 {
		t.Fatalf("sample=%+v", sink.samples[0])
	}
	assertTimeMillis(t, sink.samples[0].StartedAt, 1000)
	assertTimeMillis(t, sink.samples[0].FirstOutputAt, 1500)
	assertTimeMillis(t, sink.samples[0].CompletedAt, 3500)
}

func TestUsageTelemetryIgnoresForeignSession(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.providerID = "p"
	state.modelID = "m"
	finish := mustJSON(t, map[string]any{
		"sessionID": "other",
		"part":      map[string]any{"id": "finish", "type": "step-finish", "tokens": map[string]any{"input": 1, "output": 1, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}},
	})
	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: finish}); err != nil {
		t.Fatal(err)
	}
	if len(sink.samples) != 0 {
		t.Fatalf("foreign session samples=%+v", sink.samples)
	}
}

func TestUsageTelemetryFillsStartedAtWhenMessageUpdatedArrivesAfterStepFinish(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.seedModelUsage("openrouter", "model", nil)

	for _, update := range []map[string]any{
		{"part": map[string]any{"id": "start_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-start"}},
		{"part": map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "reasoning", "text": "", "time": map[string]any{"start": 2500, "end": 4000}}},
		{"part": map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 8498, "output": 263, "reasoning": 196, "cache": map[string]any{"read": 0, "write": 0}}}},
	} {
		if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: mustJSON(t, update)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.updated", Properties: mustJSON(t, map[string]any{
		"info": map[string]any{
			"id": "msg_1", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
			"time": map[string]any{"created": 1000, "completed": 4100},
		},
	})}); err != nil {
		t.Fatal(err)
	}

	summary := modelusage.Aggregate(sink.samples)
	if summary == nil || summary.AverageWaitMs == nil || *summary.AverageWaitMs != 1500 {
		t.Fatalf("avg wait missing after late message.updated: samples=%+v summary=%+v", sink.samples, summary)
	}
	if summary.InputTokens != 8498 || summary.OutputTokens != 263 {
		t.Fatalf("late timing enrichment replaced token totals: %+v", summary)
	}
}

func TestUsageTelemetryPrefersEarlierReasoningStartOverLaterToolStart(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.seedModelUsage("openrouter", "model", nil)

	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.updated", Properties: mustJSON(t, map[string]any{
		"info": map[string]any{
			"id": "msg_1", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
			"time": map[string]any{"created": 1000},
		},
	})}); err != nil {
		t.Fatal(err)
	}
	for _, update := range []map[string]any{
		{"part": map[string]any{"id": "start_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-start"}},
		{"part": map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "reasoning", "text": "", "time": map[string]any{}}},
		{"part": map[string]any{"id": "tool_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "tool", "tool": "write", "state": map[string]any{"status": "running", "time": map[string]any{"start": 8518}}}},
		{"part": map[string]any{"id": "reason_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "reasoning", "text": "plan", "time": map[string]any{"start": 2500, "end": 8000}}},
		{"part": map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 10, "output": 8, "reasoning": 2, "cache": map[string]any{"read": 0, "write": 0}}}},
	} {
		if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: mustJSON(t, update)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.samples) != 1 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	assertTimeMillis(t, sink.samples[0].StartedAt, 1000)
	assertTimeMillis(t, sink.samples[0].FirstOutputAt, 2500)
	assertTimeMillis(t, sink.samples[0].CompletedAt, 8000)
}

func TestUsageTelemetryUsesLaterPartEndAndIgnoresToolDuration(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.providerID = "openrouter"
	state.modelID = "model"

	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.updated", Properties: mustJSON(t, map[string]any{
		"sessionID": "ses_1",
		"info": map[string]any{
			"id": "msg_1", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
			"time": map[string]any{"created": 1000},
		},
	})}); err != nil {
		t.Fatal(err)
	}

	for _, update := range []map[string]any{
		{"sessionID": "ses_1", "part": map[string]any{"id": "start_1", "messageID": "msg_1", "type": "step-start"}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "reason_1", "messageID": "msg_1", "type": "reasoning", "text": "plan", "time": map[string]any{"start": 1200}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "txt_1", "messageID": "msg_1", "type": "text", "text": "answer", "time": map[string]any{"start": 1800, "end": 2200}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "tool_1", "messageID": "msg_1", "type": "tool", "tool": "bash", "state": map[string]any{"status": "completed", "time": map[string]any{"start": 2300, "end": 9000}}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "finish_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 10, "output": 8, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "start_2", "messageID": "msg_1", "type": "step-start"}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "txt_2", "messageID": "msg_1", "type": "text", "text": "follow up", "time": map[string]any{"start": 10000, "end": 11000}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "finish_2", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 20, "output": 4, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}}},
	} {
		if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: mustJSON(t, update)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.samples) != 2 {
		t.Fatalf("samples=%+v", sink.samples)
	}
	assertTimeMillis(t, sink.samples[0].StartedAt, 1000)
	assertTimeMillis(t, sink.samples[0].FirstOutputAt, 1200)
	assertTimeMillis(t, sink.samples[0].CompletedAt, 2200)
	if sink.samples[1].StartedAt != nil {
		t.Fatalf("later step reused the assistant message created time: %+v", sink.samples[1])
	}
	assertTimeMillis(t, sink.samples[1].FirstOutputAt, 10000)
	assertTimeMillis(t, sink.samples[1].CompletedAt, 11000)
}

func TestUsageEnrichesStartedAtAfterLateMessageUpdated(t *testing.T) {
	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.seedModelUsage("openrouter", "model", nil)

	finish := mustJSON(t, map[string]any{
		"sessionID": "ses_1",
		"part":      map[string]any{"id": "finish_1", "messageID": "msg_1", "type": "step-finish", "tokens": map[string]any{"input": 12, "output": 3, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}},
	})
	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.part.updated", Properties: finish}); err != nil {
		t.Fatal(err)
	}
	if len(sink.samples) != 1 || sink.samples[0].StartedAt != nil {
		t.Fatalf("expected unfinished sample without start: %+v", sink.samples)
	}

	late := mustJSON(t, map[string]any{
		"info": map[string]any{
			"id": "msg_1", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
			"time": map[string]any{"created": 1000},
		},
	})
	if err := state.handleEvent(t.Context(), nil, client.Event{Type: "message.updated", Properties: late}); err != nil {
		t.Fatal(err)
	}
	if len(sink.samples) != 2 {
		t.Fatalf("late start was not recorded: samples=%+v", sink.samples)
	}
	assertTimeMillis(t, sink.samples[1].StartedAt, 1000)
	if sink.samples[1].SampleID != sink.samples[0].SampleID {
		t.Fatalf("late start used a different sample: %+v", sink.samples)
	}
}

func TestUsageBackfillIgnoresHistoryFetchErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "native unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}

	state := newRunState("ses_1", nil, &recordingUsageSink{})
	state.seedModelUsage("openrouter", "model", nil)
	if err := state.backfillUsageFromHistory(t.Context(), native); err != nil {
		t.Fatalf("history fetch failure must not fail the run: %v", err)
	}
}

func TestUsageHistoryBackfillSkipsMalformedMessagesAndContinues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/ses_1/message" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(mustJSON(t, []any{
			map[string]any{"parts": []any{}},
			map[string]any{"info": map[string]any{"id": "user_1", "sessionID": "ses_1", "role": "user"}},
			map[string]any{"info": 1},
			map[string]any{
				"info": map[string]any{
					"id": "msg_done", "sessionID": "ses_1", "role": "assistant", "providerID": "openrouter", "modelID": "model",
					"time": map[string]any{"created": 1000, "completed": 4000},
				},
				"parts": []any{
					map[string]any{"id": "start_1", "sessionID": "ses_1", "messageID": "msg_done", "type": "step-start"},
					map[string]any{"id": "finish_1", "sessionID": "ses_1", "messageID": "msg_done", "type": "step-finish", "tokens": map[string]any{"input": 10, "output": 4, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}},
				},
			},
		}))
	}))
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}

	sink := &recordingUsageSink{}
	state := newRunState("ses_1", nil, sink)
	state.seedModelUsage("openrouter", "model", nil)
	if err := state.backfillUsageFromHistory(t.Context(), native); err != nil {
		t.Fatalf("malformed history must not fail the run: %v", err)
	}
	if len(sink.samples) != 1 || sink.samples[0].SampleID != "opencode:ses_1:finish_1" {
		t.Fatalf("samples=%+v", sink.samples)
	}

	emptyState := newRunState("ses_1", nil, sink)
	if err := emptyState.backfillUsageFromHistory(t.Context(), nil); err != nil {
		t.Fatalf("nil native backfill error=%v", err)
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
