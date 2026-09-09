package evidence

import (
	"context"
	"sync"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const hubSubscriberBuffer = 32

// Hub is an in-process EventPublisher. v0.1 is single-process; live delivery
// is best-effort and must never block durable persistence.
type Hub struct {
	mu          sync.Mutex
	nextID      uint64
	byRunID     map[string]map[uint64]*hubSubscriber
	byProjectID map[string]map[uint64]*hubSubscriber
}

type hubSubscriber struct {
	id        uint64
	runID     string
	projectID string
	ch        chan store.Event
}

func NewHub() *Hub {
	return &Hub{
		byRunID:     make(map[string]map[uint64]*hubSubscriber),
		byProjectID: make(map[string]map[uint64]*hubSubscriber),
	}
}

func (h *Hub) Subscribe(ctx context.Context, runID string) (<-chan store.Event, func()) {
	return h.subscribe(ctx, &hubSubscriber{runID: runID, ch: make(chan store.Event, hubSubscriberBuffer)})
}

func (h *Hub) SubscribeProject(ctx context.Context, projectID string) (<-chan store.Event, func()) {
	return h.subscribe(ctx, &hubSubscriber{projectID: projectID, ch: make(chan store.Event, hubSubscriberBuffer)})
}

func (h *Hub) subscribe(ctx context.Context, sub *hubSubscriber) (<-chan store.Event, func()) {
	if h == nil || (sub.runID == "" && sub.projectID == "") {
		idle := make(chan store.Event)
		return idle, func() {}
	}

	h.mu.Lock()
	h.nextID++
	sub.id = h.nextID
	if sub.runID != "" {
		subs := h.byRunID[sub.runID]
		if subs == nil {
			subs = make(map[uint64]*hubSubscriber)
			h.byRunID[sub.runID] = subs
		}
		subs[sub.id] = sub
	} else {
		subs := h.byProjectID[sub.projectID]
		if subs == nil {
			subs = make(map[uint64]*hubSubscriber)
			h.byProjectID[sub.projectID] = subs
		}
		subs[sub.id] = sub
	}
	h.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() { h.remove(sub) })
	}
	go func() {
		<-ctx.Done()
		unsubscribe()
	}()
	return sub.ch, unsubscribe
}

func (h *Hub) Publish(_ context.Context, event store.Event) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	snapshot := make([]*hubSubscriber, 0)
	if event.RunID != nil && *event.RunID != "" {
		for _, sub := range h.byRunID[*event.RunID] {
			snapshot = append(snapshot, sub)
		}
	}
	if event.ProjectID != "" {
		for _, sub := range h.byProjectID[event.ProjectID] {
			snapshot = append(snapshot, sub)
		}
	}
	h.mu.Unlock()

	for _, sub := range snapshot {
		deliverDropOldest(sub.ch, event)
	}
	return nil
}

func (h *Hub) remove(sub *hubSubscriber) {
	if h == nil || sub == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if sub.runID != "" {
		subs := h.byRunID[sub.runID]
		if subs == nil {
			return
		}
		delete(subs, sub.id)
		if len(subs) == 0 {
			delete(h.byRunID, sub.runID)
		}
		return
	}
	subs := h.byProjectID[sub.projectID]
	if subs == nil {
		return
	}
	delete(subs, sub.id)
	if len(subs) == 0 {
		delete(h.byProjectID, sub.projectID)
	}
}

func deliverDropOldest(ch chan store.Event, event store.Event) {
	select {
	case ch <- event:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- event:
	default:
	}
}

var _ EventPublisher = (*Hub)(nil)
