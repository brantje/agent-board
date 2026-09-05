package server

import (
	"context"
	"sync"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

// sessionDelivery decouples an execution's output stream from any single
// WebSocket connection. A disconnected control-plane client can reconnect and
// receive the remaining output and exit result without restarting execution.
type sessionDelivery struct {
	ctx context.Context

	mu      sync.Mutex
	writer  *connectionWriter
	changed chan struct{}
}

func newSessionDelivery(ctx context.Context, writer *connectionWriter) *sessionDelivery {
	return &sessionDelivery{
		ctx:     ctx,
		writer:  writer,
		changed: make(chan struct{}),
	}
}

func (d *sessionDelivery) attach(writer *connectionWriter) {
	d.mu.Lock()
	d.writer = writer
	d.notifyLocked()
	d.mu.Unlock()
}

func (d *sessionDelivery) detach(writer *connectionWriter) {
	d.mu.Lock()
	if d.writer == writer {
		d.writer = nil
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
		writer := d.writer
		changed := d.changed
		d.mu.Unlock()
		if writer != nil {
			return writer, nil
		}

		select {
		case <-d.ctx.Done():
			return nil, d.ctx.Err()
		case <-changed:
		}
	}
}

func (d *sessionDelivery) notifyLocked() {
	close(d.changed)
	d.changed = make(chan struct{})
}
