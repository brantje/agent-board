package runner

import (
	"errors"
	"net"
	"os"
	"sync"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

var errSessionWriteDeadlineChanged = errors.New("runner: session write deadline changed")

func (c *Connection) writeSessionMessage(
	typ protocol.MessageType,
	sessionID string,
	payload any,
	sessionDeadline time.Time,
	deadlineWake <-chan struct{},
	sessionDone <-chan struct{},
) error {
	msg, err := protocol.NewMessage(protocol.Version1, typ, sessionID, payload)
	if err != nil {
		return err
	}
	data, err := protocol.Encode(msg)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(writeTimeout)
	if !sessionDeadline.IsZero() && sessionDeadline.Before(deadline) {
		deadline = sessionDeadline
	}
	if !time.Now().Before(deadline) {
		return os.ErrDeadlineExceeded
	}

	unlock, err := lockWriteMutexUntil(&c.writeMu, deadline, deadlineWake, sessionDone, c.done)
	if err != nil {
		if errors.Is(err, ErrDisconnected) {
			return c.connectionError()
		}
		return err
	}
	defer unlock()

	select {
	case <-deadlineWake:
		return errSessionWriteDeadlineChanged
	default:
	}
	select {
	case <-sessionDone:
		return net.ErrClosed
	default:
	}
	select {
	case <-c.done:
		return c.connectionError()
	default:
	}
	if !time.Now().Before(deadline) {
		return os.ErrDeadlineExceeded
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func lockWriteMutexUntil(
	mu *sync.Mutex,
	deadline time.Time,
	deadlineWake <-chan struct{},
	sessionDone <-chan struct{},
	connectionDone <-chan struct{},
) (func(), error) {
	acquired := make(chan struct{})
	abandon := make(chan struct{})
	go func() {
		mu.Lock()
		select {
		case acquired <- struct{}{}:
		case <-abandon:
			mu.Unlock()
		}
	}()

	duration := time.Until(deadline)
	if duration <= 0 {
		close(abandon)
		return nil, os.ErrDeadlineExceeded
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-acquired:
		return mu.Unlock, nil
	case <-deadlineWake:
		close(abandon)
		return nil, errSessionWriteDeadlineChanged
	case <-sessionDone:
		close(abandon)
		return nil, net.ErrClosed
	case <-connectionDone:
		close(abandon)
		return nil, ErrDisconnected
	case <-timer.C:
		close(abandon)
		return nil, os.ErrDeadlineExceeded
	}
}
