package server

import (
	"errors"
	"io"
	"sync"
)

const stdinQueueCapacity = 8

var (
	errStdinQueueFull = errors.New("stdin queue is full")
	errStdinClosed    = errors.New("stdin is closed")
)

// stdinPump keeps potentially blocking process stdin writes off the WebSocket
// receive loop while preserving the order of accepted input chunks.
type stdinPump struct {
	input io.WriteCloser
	queue chan []byte

	mu          sync.Mutex
	accepting   bool
	queueClosed bool
	writeErr    error
}

func newStdinPump(input io.WriteCloser) *stdinPump {
	pump := &stdinPump{
		input:     input,
		queue:     make(chan []byte, stdinQueueCapacity),
		accepting: true,
	}
	go pump.run()
	return pump
}

func (p *stdinPump) Enqueue(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.accepting {
		if p.writeErr != nil {
			return p.writeErr
		}
		return errStdinClosed
	}

	chunk := append([]byte(nil), data...)
	select {
	case p.queue <- chunk:
		return nil
	default:
		return errStdinQueueFull
	}
}

func (p *stdinPump) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.writeErr != nil {
		return p.writeErr
	}
	if !p.accepting {
		return nil
	}
	p.accepting = false
	p.closeQueueLocked()
	return nil
}

func (p *stdinPump) run() {
	for data := range p.queue {
		if _, err := p.input.Write(data); err != nil {
			p.fail(err)
			break
		}
	}
	for range p.queue {
	}
	_ = p.input.Close()
}

func (p *stdinPump) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.writeErr == nil {
		p.writeErr = err
	}
	p.accepting = false
	p.closeQueueLocked()
}

func (p *stdinPump) closeQueueLocked() {
	if p.queueClosed {
		return
	}
	close(p.queue)
	p.queueClosed = true
}
