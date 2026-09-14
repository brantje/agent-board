package runexec

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// PublishPersisted lets the scheduler reuse the Processor's existing Event
// recorder for Events that the scheduler store has already committed.
func (p *Processor) PublishPersisted(ctx context.Context, event store.Event) {
	if p == nil || p.events == nil {
		return
	}
	p.events.PublishPersisted(ctx, event)
}
