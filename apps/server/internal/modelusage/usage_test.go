package modelusage

import (
	"math"
	"testing"
	"time"
)

func TestAggregateRunUsage(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	limit := int64(200000)
	firstStart := base
	firstOutput := base.Add(2 * time.Second)
	firstComplete := base.Add(4 * time.Second)
	secondStart := base.Add(10 * time.Second)
	secondOutput := base.Add(11 * time.Second)
	secondComplete := base.Add(13 * time.Second)

	summary := Aggregate([]Sample{
		{SampleID: "one", ProviderID: "openrouter", ModelID: "model", InputTokens: 100, OutputTokens: 20, CacheReadTokens: 40, ContextTokens: 170, ContextLimitTokens: &limit, StartedAt: &firstStart, FirstOutputAt: &firstOutput, CompletedAt: &firstComplete},
		{SampleID: "one", ProviderID: "openrouter", ModelID: "model", InputTokens: 999, OutputTokens: 999, ContextTokens: 999},
		{SampleID: "two", ProviderID: "openrouter", ModelID: "model", InputTokens: 200, OutputTokens: 60, CacheReadTokens: 50, ContextTokens: 320, ContextLimitTokens: &limit, StartedAt: &secondStart, FirstOutputAt: &secondOutput, CompletedAt: &secondComplete},
	})
	if summary == nil {
		t.Fatal("Aggregate() = nil")
	}
	if summary.InputTokens != 300 || summary.OutputTokens != 80 || summary.CacheReadTokens != 90 {
		t.Fatalf("totals=%+v", summary)
	}
	if summary.ContextTokens != 320 || summary.ContextLimitTokens == nil || *summary.ContextLimitTokens != limit {
		t.Fatalf("context=%+v", summary)
	}
	if summary.AverageWaitMs == nil || *summary.AverageWaitMs != 1500 {
		t.Fatalf("average wait=%v", summary.AverageWaitMs)
	}
	if summary.TokensPerSecond == nil || math.Abs(*summary.TokensPerSecond-20) > 0.0001 {
		t.Fatalf("tokens/sec=%v", summary.TokensPerSecond)
	}
}

func TestAggregateMergesTimingFromLaterDuplicateSamples(t *testing.T) {
	start := time.UnixMilli(1000).UTC()
	firstOutput := time.UnixMilli(2500).UTC()
	complete := time.UnixMilli(4000).UTC()
	lateOutput := time.UnixMilli(3800).UTC()

	summary := Aggregate([]Sample{
		{SampleID: "one", ProviderID: "openrouter", ModelID: "model", InputTokens: 8498, OutputTokens: 263, ContextTokens: 8957},
		{SampleID: "one", ProviderID: "openrouter", ModelID: "model", InputTokens: 1, OutputTokens: 1, ContextTokens: 2, StartedAt: &start, FirstOutputAt: &lateOutput, CompletedAt: &complete},
		{SampleID: "one", ProviderID: "openrouter", ModelID: "model", InputTokens: 1, OutputTokens: 1, ContextTokens: 2, FirstOutputAt: &firstOutput},
	})
	if summary == nil {
		t.Fatal("Aggregate() = nil")
	}
	if summary.InputTokens != 8498 || summary.OutputTokens != 263 || summary.ContextTokens != 8957 {
		t.Fatalf("duplicate tokens were counted or replaced: %+v", summary)
	}
	if summary.AverageWaitMs == nil || *summary.AverageWaitMs != 1500 {
		t.Fatalf("average wait=%v", summary.AverageWaitMs)
	}
	if summary.TokensPerSecond == nil {
		t.Fatal("tokens/sec is nil")
	}
}

func TestAggregateSkipsInvalidAndMissingTiming(t *testing.T) {
	limit := int64(1000)
	summary := Aggregate([]Sample{
		{SampleID: "bad", ProviderID: "p", ModelID: "m", InputTokens: -1},
		{SampleID: "good", ProviderID: "p", ModelID: "m", InputTokens: 10, OutputTokens: 5, ContextTokens: 15, ContextLimitTokens: &limit},
	})
	if summary == nil || summary.InputTokens != 10 || summary.AverageWaitMs != nil || summary.TokensPerSecond != nil {
		t.Fatalf("summary=%+v", summary)
	}
	if Aggregate(nil) != nil {
		t.Fatal("empty aggregate must be nil")
	}
}

func TestSampleValidateRejectsMissingIdentityAndInvalidLimit(t *testing.T) {
	if err := (Sample{}).Validate(); err == nil {
		t.Fatal("missing sample id accepted")
	}
	zero := int64(0)
	if err := (Sample{SampleID: "id", ProviderID: "p", ModelID: "m", ContextLimitTokens: &zero}).Validate(); err == nil {
		t.Fatal("zero context limit accepted")
	}
}
