package modelusage

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Sample is one completed model step normalized by an Engine adapter.
type Sample struct {
	SampleID           string     `json:"sampleId"`
	ProviderID         string     `json:"providerId"`
	ModelID            string     `json:"modelId"`
	InputTokens        int64      `json:"inputTokens"`
	OutputTokens       int64      `json:"outputTokens"`
	ReasoningTokens    int64      `json:"reasoningTokens"`
	CacheReadTokens    int64      `json:"cacheReadTokens"`
	CacheWriteTokens   int64      `json:"cacheWriteTokens"`
	ContextTokens      int64      `json:"contextTokens"`
	ContextLimitTokens *int64     `json:"contextLimitTokens,omitempty"`
	StartedAt          *time.Time `json:"startedAt,omitempty"`
	FirstOutputAt      *time.Time `json:"firstOutputAt,omitempty"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
}

// Summary is the Run-level projection used by the API and UI.
type Summary struct {
	ContextTokens      int64    `json:"contextTokens"`
	ContextLimitTokens *int64   `json:"contextLimitTokens"`
	InputTokens        int64    `json:"inputTokens"`
	OutputTokens       int64    `json:"outputTokens"`
	CacheReadTokens    int64    `json:"cacheReadTokens"`
	AverageWaitMs      *float64 `json:"averageWaitMs"`
	TokensPerSecond    *float64 `json:"tokensPerSecond"`
}

func (s Sample) Validate() error {
	if strings.TrimSpace(s.SampleID) == "" {
		return fmt.Errorf("sample id is required")
	}
	if strings.TrimSpace(s.ProviderID) == "" || strings.TrimSpace(s.ModelID) == "" {
		return fmt.Errorf("provider id and model id are required")
	}
	for name, value := range map[string]int64{
		"input tokens":       s.InputTokens,
		"output tokens":      s.OutputTokens,
		"reasoning tokens":   s.ReasoningTokens,
		"cache read tokens":  s.CacheReadTokens,
		"cache write tokens": s.CacheWriteTokens,
		"context tokens":     s.ContextTokens,
	} {
		if value < 0 {
			return fmt.Errorf("%s must be >= 0", name)
		}
	}
	if s.ContextLimitTokens != nil && *s.ContextLimitTokens <= 0 {
		return fmt.Errorf("context limit tokens must be > 0")
	}
	return nil
}

// Aggregate deduplicates samples by SampleID and projects the requested Run metrics.
// Input order is authoritative, matching the persisted Run Event sequence.
func Aggregate(samples []Sample) *Summary {
	seen := make(map[string]struct{}, len(samples))
	valid := make([]Sample, 0, len(samples))
	for _, sample := range samples {
		if sample.Validate() != nil {
			continue
		}
		if _, duplicate := seen[sample.SampleID]; duplicate {
			continue
		}
		seen[sample.SampleID] = struct{}{}
		valid = append(valid, sample)
	}
	if len(valid) == 0 {
		return nil
	}

	summary := &Summary{}
	var waitTotal float64
	var waitCount int
	var rateTotal float64
	var rateCount int
	for _, sample := range valid {
		summary.InputTokens += sample.InputTokens
		summary.OutputTokens += sample.OutputTokens
		summary.CacheReadTokens += sample.CacheReadTokens
		summary.ContextTokens = sample.ContextTokens
		summary.ContextLimitTokens = cloneInt64(sample.ContextLimitTokens)

		if sample.StartedAt != nil && sample.FirstOutputAt != nil {
			wait := sample.FirstOutputAt.Sub(*sample.StartedAt)
			if wait >= 0 {
				waitTotal += float64(wait) / float64(time.Millisecond)
				waitCount++
			}
		}
		if sample.OutputTokens > 0 && sample.FirstOutputAt != nil && sample.CompletedAt != nil {
			generation := sample.CompletedAt.Sub(*sample.FirstOutputAt)
			if generation > 0 {
				rate := float64(sample.OutputTokens) / generation.Seconds()
				if !math.IsInf(rate, 0) && !math.IsNaN(rate) {
					rateTotal += rate
					rateCount++
				}
			}
		}
	}
	if waitCount > 0 {
		value := waitTotal / float64(waitCount)
		summary.AverageWaitMs = &value
	}
	if rateCount > 0 {
		value := rateTotal / float64(rateCount)
		summary.TokensPerSecond = &value
	}
	return summary
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
