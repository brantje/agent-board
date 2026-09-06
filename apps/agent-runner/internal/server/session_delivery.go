package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

const defaultDeliveryReconnectRetention = 60 * time.Second

var errDeliveryExpired = errors.New("session delivery reconnect window expired")

// sessionDelivery decouples an execution's output stream from any single
// WebSocket connection. A disconnected control-plane client can reconnect and
// receive the remaining output and exit result without restarting execution.
// Detached delivery state is retained for a bounded reconnect window so stale
// completed sessions cannot retain goroutines indefinitely.
type sessionDelivery struct {
	ctx       context.Context
	retention time.Duration

	mu        sync.Mutex
	writer    *connectionWriter
	changed   chan struct{}
	expiresAt time.Time
	expired   bool
}

func newSessionDelivery(ctx context.Context, writer *connectionWriter, retentionOverride ...time.Duration) *sessionDelivery {
	retention := defaultDeliveryReconnectRetention
	if len(retentionOverride) > 0 && retentionOverride[0] > 0 {
		retention = retentionOverride[0]
	}
	delivery := &sessionDelivery{
		ctx:       ctx,
		retention: retention,
		writer:    writer,
		changed:   make(chan struct{}),
	}
	if writer == nil {
		delivery.expiresAt = time.Now().Add(retention)
	}
	return delivery
}

func (d *sessionDelivery) attach(writer *connectionWriter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.expiredLocked(time.Now()) {
		return
	}
	d.writer = writer
	d.expiresAt = time.Time{}
	d.notifyLocked()
}

func (d *sessionDelivery) attachIfIdle(writer *connectionWriter) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writer != nil || d.expiredLocked(time.Now()) {
		return false
	}
	d.writer = writer
	d.expiresAt = time.Time{}
	d.notifyLocked()
	return true
}

func (d *sessionDelivery) detach(writer *connectionWriter) {
	d.mu.Lock()
	if d.writer == writer {
		d.writer = nil
		if !d.expired {
			d.expiresAt = time.Now().Add(d.retention)
		}
		d.notifyLocked()
	}
	d.mu.Unlock()
}

func (d *sessionDelivery) send(typ protocol.MessageType, sessionID string, payload any) error {
	for {
		writer, err := d.waitWriter()
		if err != nil {
			return err
		}
		if err := writer.send(typ, sessionID, payload); err == nil {
			return nil
		}
		d.detach(writer)
	}
}

func (d *sessionDelivery) sendError(code, message, sessionID string) {
	_ = d.send(protocol.TypeError, sessionID, protocol.ErrorPayload{Code: code, Message: message})
}

func (d *sessionDelivery) waitWriter() (*connectionWriter, error) {
	for {
		d.mu.Lock()
		now := time.Now()
		if d.expiredLocked(now) {
			d.mu.Unlock()
			return nil, errDeliveryExpired
		}
		writer := d.writer
		changed := d.changed
		deadline := d.expiresAt
		if writer == nil && deadline.IsZero() {
			deadline = now.Add(d.retention)
			d.expiresAt = deadline
		}
		d.mu.Unlock()
		if writer != nil {
			return writer, nil
		}

		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-d.ctx.Done():
			stopTimer(timer)
			return nil, d.ctx.Err()
		case <-changed:
			stopTimer(timer)
		case <-timer.C:
		}
	}
}

func (d *sessionDelivery) expiredLocked(now time.Time) bool {
	if d.expired {
		return true
	}
	if d.writer == nil && !d.expiresAt.IsZero() && !now.Before(d.expiresAt) {
		d.expired = true
		return true
	}
	return false
}

func (d *sessionDelivery) notifyLocked() {
	close(d.changed)
	d.changed = make(chan struct{})
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
