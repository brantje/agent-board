package app

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/modelusage"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

// RunUsage is the normalized Run-level model telemetry projection exposed by
// Run evidence and the live usage endpoint.
type RunUsage = modelusage.Summary

func (s *RunEvidenceService) Usage(ctx context.Context, projectID, runID string) (*RunUsage, error) {
	if _, err := s.requireRun(ctx, projectID, runID); err != nil {
		return nil, err
	}
	events, err := s.listAllRunEvents(ctx, projectID, runID)
	if err != nil {
		return nil, err
	}
	return runUsageFromEvents(events), nil
}

func runUsageFromEvents(events []store.Event) *RunUsage {
	samples := make([]modelusage.Sample, 0)
	for _, event := range events {
		if event.Type != "model.usage" {
			continue
		}
		var sample modelusage.Sample
		if err := json.Unmarshal(event.Payload, &sample); err != nil {
			continue
		}
		samples = append(samples, sample)
	}
	return modelusage.Aggregate(samples)
}
