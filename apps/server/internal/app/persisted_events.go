package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type persistedEventPublisher interface {
	PublishPersisted(context.Context, store.Event)
}

func publishPersistedEvents(ctx context.Context, publisher persistedEventPublisher, events []store.Event) {
	if publisher == nil {
		return
	}
	for _, event := range events {
		publisher.PublishPersisted(ctx, event)
	}
}
