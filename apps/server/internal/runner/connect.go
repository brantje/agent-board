package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

const (
	sessionConnectFeature = "session_connect"
	connectWriteChunkSize = 32 * 1024
	connectReadQueueDepth = 32
)

var (
	ErrSessionConnectUnsupported  = errors.New("runner: session-local connections unsupported")
	ErrSessionConnectBackpressure = errors.New("runner: session-local connection backpressure")
)

// DialSession opens a connection from agent-runner to a service local to the
// Runtime that owns sessionID. The returned net.Conn is transported through the
// runner WebSocket; Runtime networking details remain private to agent-runner.
func (c *Connection) DialSession(ctx context.Context, sessionID, network, address string) (net.Conn, error) {
	if c == nil {
		return nil, ErrClosed
	}
	if sessionID == "" {
		return nil, fmt.Errorf("runner: session id is required")
	}
	if !c.supportsFeature(sessionConnectFeature) {
		return nil, ErrSessionConnectUnsupported
	}
	connectionID, err := newSessionConnectionID()
	if err != nil {
		return nil, err
	}
	connection := newSessionConn(c, sessionID, connectionID, network, address)
	if err := c.registerConnect(connection); err != nil {
		return nil, err
	}
	if err := c.write(protocol.TypeConnect, sessionID, protocol.ConnectRequest{
		ConnectionID: connectionID,
		Network:      network,
		Address:      address,
	}); err != nil {
		c.removeConnect(connection)
		connection.closeRemote(err)
		return nil, err
	}

	select {
	case err := <-connection.connected:
		if err != nil {
			c.removeConnect(connection)
			return nil, err
		}
		return connection, nil
	case <-ctx.Done():
		connection.closeLocal(ctx.Err())
		return nil, ctx.Err()
	case <-c.done:
		connection.closeRemote(c.connectionError())
		return nil, c.connectionError()
	}
}

func (c *Connection) supportsFeature(feature string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, candidate := range c.caps.Features {
		if candidate == feature {
			return true
		}
	}
	return false
}

func (c *Connection) registerConnect(connection *sessionConn) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	byID := c.connects[connection.sessionID]
	if byID == nil {
		byID = make(map[string]*sessionConn)
		c.connects[connection.sessionID] = byID
	}
	if _, exists := byID[connection.id]; exists {
		return fmt.Errorf("runner: duplicate session connection id")
	}
	byID[connection.id] = connection
	return nil
}

func (c *Connection) removeConnect(connection *sessionConn) {
	c.mu.Lock()
	byID := c.connects[connection.sessionID]
	if byID != nil && byID[connection.id] == connection {
		delete(byID, connection.id)
		if len(byID) == 0 {
			delete(c.connects, connection.sessionID)
		}
	}
	c.mu.Unlock()
}

func (c *Connection) findConnect(sessionID, connectionID string) *sessionConn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connects[sessionID][connectionID]
}

func (c *Connection) handleConnectMessage(msg protocol.Message) error {
	var connectionID string
	switch msg.Type {
	case protocol.TypeConnected:
		payload, err := protocol.DecodePayload[protocol.Connected](msg)
		if err != nil {
			return fmt.Errorf("runner: decode connected: %w", err)
		}
		connectionID = payload.ConnectionID
		if connection := c.findConnect(msg.SessionID, connectionID); connection != nil {
			connection.markConnected(nil)
		}
		return nil
	case protocol.TypeConnectData:
		payload, err := protocol.DecodePayload[protocol.ConnectData](msg)
		if err != nil {
			return fmt.Errorf("runner: decode connect data: %w", err)
		}
		connectionID = payload.ConnectionID
		if connection := c.findConnect(msg.SessionID, connectionID); connection != nil {
			if !connection.push(payload.Data) {
				connection.closeLocal(ErrSessionConnectBackpressure)
			}
		}
		return nil
	case protocol.TypeConnectClose:
		payload, err := protocol.DecodePayload[protocol.ConnectClose](msg)
		if err != nil {
			return fmt.Errorf("runner: decode connect close: %w", err)
		}
		connectionID = payload.ConnectionID
		connection := c.findConnect(msg.SessionID, connectionID)
		if connection == nil {
			return nil
		}
		var closeErr error = io.EOF
		if payload.Code != "" {
			closeErr = &ProtocolError{Code: payload.Code, Message: payload.Message}
		}
		connection.markConnected(closeErr)
		c.removeConnect(connection)
		connection.closeRemote(closeErr)
		return nil
	default:
		return fmt.Errorf("runner: unexpected connection message type %s", msg.Type)
	}
}

func newSessionConnectionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("runner: create session connection id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

type sessionConn struct {
	parent    *Connection
	sessionID string
	id        string
	network   string
	address   string

	incoming    chan []byte
	connected   chan error
	done        chan struct{}
	closeOnce   sync.Once
	connectOnce sync.Once

	mu            sync.Mutex
	current       []byte
	closeErr      error
	readDeadline  time.Time
	writeDeadline time.Time
}

func newSessionConn(parent *Connection, sessionID, id, network, address string) *sessionConn {
	return &sessionConn{
		parent:    parent,
		sessionID: sessionID,
		id:        id,
		network:   network,
		address:   address,
		incoming:  make(chan []byte, connectReadQueueDepth),
		connected: make(chan error, 1),
		done:      make(chan struct{}),
	}
}

func (c *sessionConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		c.mu.Lock()
		if len(c.current) > 0 {
			n := copy(p, c.current)
			c.current = c.current[n:]
			c.mu.Unlock()
			return n, nil
		}
		deadline := c.readDeadline
		c.mu.Unlock()

		select {
		case chunk := <-c.incoming:
			if len(chunk) == 0 {
				continue
			}
			c.mu.Lock()
			c.current = chunk
			c.mu.Unlock()
			continue
		default:
		}

		var timer <-chan time.Time
		var stopTimer func()
		if !deadline.IsZero() {
			duration := time.Until(deadline)
			if duration <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			t := time.NewTimer(duration)
			timer = t.C
			stopTimer = func() {
				if !t.Stop() {
					select {
					case <-t.C:
					default:
					}
				}
			}
		} else {
			stopTimer = func() {}
		}

		select {
		case chunk := <-c.incoming:
			stopTimer()
			if len(chunk) == 0 {
				continue
			}
			c.mu.Lock()
			c.current = chunk
			c.mu.Unlock()
		case <-c.done:
			stopTimer()
			return 0, c.readCloseError()
		case <-timer:
			return 0, os.ErrDeadlineExceeded
		}
	}
}

func (c *sessionConn) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		select {
		case <-c.done:
			return written, c.writeCloseError()
		default:
		}
		c.mu.Lock()
		deadline := c.writeDeadline
		c.mu.Unlock()
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return written, os.ErrDeadlineExceeded
		}
		size := len(p)
		if size > connectWriteChunkSize {
			size = connectWriteChunkSize
		}
		chunk := append([]byte(nil), p[:size]...)
		if err := c.parent.write(protocol.TypeConnectData, c.sessionID, protocol.ConnectData{ConnectionID: c.id, Data: chunk}); err != nil {
			c.closeRemote(err)
			return written, err
		}
		written += size
		p = p[size:]
	}
	return written, nil
}

func (c *sessionConn) Close() error {
	c.closeLocal(io.EOF)
	return nil
}

func (c *sessionConn) LocalAddr() net.Addr {
	return sessionAddr{network: c.network, address: "agent-board"}
}
func (c *sessionConn) RemoteAddr() net.Addr {
	return sessionAddr{network: c.network, address: c.address}
}

func (c *sessionConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.readDeadline = deadline
	c.writeDeadline = deadline
	c.mu.Unlock()
	return nil
}

func (c *sessionConn) SetReadDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.readDeadline = deadline
	c.mu.Unlock()
	return nil
}

func (c *sessionConn) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.writeDeadline = deadline
	c.mu.Unlock()
	return nil
}

func (c *sessionConn) push(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	chunk := append([]byte(nil), data...)
	select {
	case <-c.done:
		return false
	case c.incoming <- chunk:
		return true
	default:
		return false
	}
}

func (c *sessionConn) markConnected(err error) {
	c.connectOnce.Do(func() { c.connected <- err })
}

func (c *sessionConn) closeLocal(err error) {
	c.closeOnce.Do(func() {
		c.parent.removeConnect(c)
		c.setCloseError(err)
		close(c.done)
		c.markConnected(err)

		// net.Conn.Close and DialSession cancellation must not wait on a stale
		// runner transport. The shared writer still bounds this best-effort close
		// notification with the normal WebSocket write timeout.
		go func() {
			_ = c.parent.write(protocol.TypeConnectClose, c.sessionID, protocol.ConnectClose{ConnectionID: c.id})
		}()
	})
}

func (c *sessionConn) closeRemote(err error) {
	c.closeOnce.Do(func() {
		c.parent.removeConnect(c)
		c.setCloseError(err)
		close(c.done)
		c.markConnected(err)
	})
}

func (c *sessionConn) setCloseError(err error) {
	if err == nil {
		err = io.EOF
	}
	c.mu.Lock()
	c.closeErr = err
	c.mu.Unlock()
}

func (c *sessionConn) readCloseError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeErr == nil || errors.Is(c.closeErr, io.EOF) {
		return io.EOF
	}
	return c.closeErr
}

func (c *sessionConn) writeCloseError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeErr == nil || errors.Is(c.closeErr, io.EOF) {
		return net.ErrClosed
	}
	return c.closeErr
}

type sessionAddr struct {
	network string
	address string
}

func (a sessionAddr) Network() string { return a.network }
func (a sessionAddr) String() string  { return a.address }
