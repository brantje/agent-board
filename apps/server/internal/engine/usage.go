package engine

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/modelusage"
)

// UsageSink is an optional server-owned capability for normalized model usage.
// Engine adapters translate native telemetry into samples; persistence and Run
// aggregation remain control-plane responsibilities.
type UsageSink interface {
	RecordModelUsage(context.Context, modelusage.Sample) error
}
