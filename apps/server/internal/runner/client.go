package runner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

const (
	maxMessageSize            = 1 << 20
	writeTimeout              = 10 * time.Second
	handshakeTimeout          = 10 * time.Second
	runnerReadTimeout         = 90 * time.Second
	maxPendingSessions        = 8
	maxPendingSessionMessages = 32
	maxPendingSessionBytes    = 2 << 20
)

var (
	ErrDisconnected       = errors.New("runner: disconnected")
	ErrIncompatibleRunner = errors.New("runner: incompatible runner")
	ErrSessionExists      = errors.New("runner: session already registered")
	ErrClosed             = errors.New("runner: connection closed")
)

type ProtocolError struct {
	Code    string
	Message string
}

func (e *ProtocolError) Error() string {
	if e.Message == "" {
		return "runner protocol error: " + e.Code
	}
	return fmt.Sprintf("runner protocol error %s: %s", e.Code, e.Message)
}

type pendingSessionMessages struct {
	messages []protocol.Message
	bytes    int
}

type Connection struct {
	conn *websocket.Conn

	writeMu   sync.Mutex
	mu        sync.RWMutex
	sessions  map[string]*Session
	pending   map[string]*pendingSessionMessages
	health    protocol.Health
	caps      protocol.Capabilities
	err       error
	done      chan struct{}
	closeOnce sync.Once
}

func Dial(ctx context.Context, endpoint string) (*Connection, error) {
	return dialWith(ctx, websocket.DefaultDialer, endpoint, nil)
}

func dialWith(ctx context.Context, dialer *websocket.Dialer, endpoint string, header http.Header) (*Connection, error) {
	if dialer == nil {
		return nil, fmt.Errorf("runner dialer is required")
	}
	conn, response, err := dialer.DialContext(ctx, endpoint, header)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, fmt.Errorf("dial runner: %w", err)
	}
	c := &Connection{
		conn:     conn,
		sessions: make(map[string]*Session),
		pending:  make(map[string]*pendingSessionMessages),
		done:     make(chan struct{}),
	}
	conn.SetReadLimit(maxMessageSize)
	if err := c.handshake(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := c.configureLiveness(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	go c.readLoop()
	return c, nil
}

func (c *Connection) handshake(ctx context.Context) error {
	deadline := time.Now().Add(handshakeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok {
		deadline = ctxDeadline
	}
	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("set runner handshake deadline: %w", err)
	}
	defer func() { _ = c.conn.SetReadDeadline(time.Time{}) }()

	if err := c.write(protocol.TypeServerHello, "", protocol.ServerHello{SupportedVersions: []int{protocol.Version1}}); err != nil {
		return fmt.Errorf("send runner hello: %w", err)
	}
	msg, err := c.readProtocolMessage()
	if err != nil {
		return fmt.Errorf("read runner hello: %w", err)
	}
	if msg.Type == protocol.TypeError {
		return protocolErrorFromMessage(msg)
	}
	if msg.Type != protocol.TypeRunnerHello {
		return fmt.Errorf("%w: expected runner_hello, got %s", ErrIncompatibleRunner, msg.Type)
	}
	hello, err := protocol.DecodePayload[protocol.RunnerHello](msg)
	if err != nil {
		return fmt.Errorf("%w: invalid runner hello: %v", ErrIncompatibleRunner, err)
	}
	if hello.Version != protocol.Version1 || hello.Capabilities.MaxActiveSessions < 1 {
		return fmt.Errorf("%w: version=%d max_active_sessions=%d", ErrIncompatibleRunner, hello.Version, hello.Capabilities.MaxActiveSessions)
	}
	if err := validateFeatures(hello.Capabilities.Features); err != nil {
		return err
	}

	healthMessage, err := c.readProtocolMessage()
	if err != nil {
		return fmt.Errorf("read runner health: %w", err)
	}
	if healthMessage.Type != protocol.TypeHealth {
		return fmt.Errorf("%w: expected initial health, got %s", ErrIncompatibleRunner, healthMessage.Type)
	}
	health, err := protocol.DecodePayload[protocol.Health](healthMessage)
	if err != nil {
		return fmt.Errorf("%w: invalid health payload: %v", ErrIncompatibleRunner, err)
	}
	c.caps = hello.Capabilities
	c.health = health
	return nil
}

func (c *Connection) configureLiveness() error {
	if err := c.conn.SetReadDeadline(time.Now().Add(runnerReadTimeout)); err != nil {
		return fmt.Errorf("set runner read deadline: %w", err)
	}
	defaultPingHandler := c.conn.PingHandler()
	c.conn.SetPingHandler(func(message string) error {
		if err := c.conn.SetReadDeadline(time.Now().Add(runnerReadTimeout)); err != nil {
			return err
		}
		return defaultPingHandler(message)
	})
	return nil
}

func validateFeatures(features []string) error {
	required := map[string]bool{
		"stdin":     false,
		"stdout":    false,
		"stderr":    false,
		"terminate": false,
		"kill":      false,
		"health":    false,
	}
	for _, feature := range features {
		if _, ok := required[feature]; ok {
			required[feature] = true
		}
	}
	for feature, present := range required {
		if !present {
			return fmt.Errorf("%w: runner missing %s capability", ErrIncompatibleRunner, feature)
		}
	}
	return nil
}

func (c *Connection) Capabilities() protocol.Capabilities {
	c.mu.RLock()
	defer c.mu.RUnlock()
	caps := c.caps
	caps.Features = append([]string(nil), caps.Features...)
	return caps
}

func (c *Connection) Health() protocol.Health {
	c.mu.RLock()
	defer c.mu.RUnlock()
	health := c.health
	health.ActiveSessionIDs = append([]string(nil), health.ActiveSessionIDs...)
	return health
}

func (c *Connection) Done() <-chan struct{} { return c.done }

func (c *Connection) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}

func (c *Connection) Start(ctx context.Context, sessionID string, request Request) (ProcessSession, error) {
	session, err := c.register(sessionID)
	if err != nil {
		return nil, err
	}
	if err := c.write(protocol.TypeStart, sessionID, protocol.StartRequest{
		Command: request.Command,
		Dir:     request.Dir,
		Env:     request.Env,
		Secrets: request.Secrets,
	}); err != nil {
		c.unregister(sessionID, session)
		session.fail(fmt.Errorf("start session: %w", err))
		return nil, err
	}
	select {
	case err := <-session.started:
		if err != nil {
			c.unregister(sessionID, session)
			return nil, err
		}
		return session, nil
	case <-ctx.Done():
		// The start request was already sent, so ownership is uncertain. Return
		// the registered session with the context error so the service can keep
		// observing it instead of losing the only same-connection consumer.
		return session, ctx.Err()
	case <-c.done:
		return nil, c.connectionError()
	}
}

// Attach registers the server-side consumer for a session that may already be
// executing, or whose retained terminal delivery may already have arrived on a
// freshly reconnected WebSocket.
func (c *Connection) Attach(sessionID string) (ProcessSession, error) {
	session, err := c.register(sessionID)
	if err != nil {
		return nil, err
	}
	session.markStarted(nil)
	return session, nil
}

func (c *Connection) register(sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("runner: session id is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	if _, exists := c.sessions[sessionID]; exists {
		return nil, ErrSessionExists
	}
	session := newSession(sessionID, c)
	c.sessions[sessionID] = session
	if pending := c.pending[sessionID]; pending != nil {
		delete(c.pending, sessionID)
		for _, msg := range pending.messages {
			terminal, err := deliverSessionMessage(session, msg)
			if err != nil {
				session.fail(err)
				delete(c.sessions, sessionID)
				break
			}
			if terminal {
				delete(c.sessions, sessionID)
				break
			}
		}
	}
	return session, nil
}

func (c *Connection) unregister(sessionID string, session *Session) {
	c.mu.Lock()
	if c.sessions[sessionID] == session {
		delete(c.sessions, sessionID)
	}
	c.mu.Unlock()
}

func (c *Connection) readLoop() {
	for {
		msg, err := c.readProtocolMessage()
		if err != nil {
			c.fail(fmt.Errorf("%w: %v", ErrDisconnected, err))
			return
		}
		if err := c.handleMessage(msg); err != nil {
			c.fail(err)
			return
		}
	}
}

func (c *Connection) handleMessage(msg protocol.Message) error {
	if msg.Type == protocol.TypeHealth {
		health, err := protocol.DecodePayload[protocol.Health](msg)
		if err != nil {
			return fmt.Errorf("runner: decode health: %w", err)
		}
		c.mu.Lock()
		c.health = health
		c.mu.Unlock()
		return nil
	}
	if msg.Type == protocol.TypeError && msg.SessionID == "" {
		return protocolErrorFromMessage(msg)
	}

	c.mu.Lock()
	session := c.sessions[msg.SessionID]
	if session == nil {
		err := c.bufferPendingLocked(msg)
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	terminal, err := deliverSessionMessage(session, msg)
	if err != nil {
		session.fail(err)
		c.unregister(msg.SessionID, session)
		return nil
	}
	if terminal {
		c.unregister(msg.SessionID, session)
	}
	return nil
}

func (c *Connection) bufferPendingLocked(msg protocol.Message) error {
	switch msg.Type {
	case protocol.TypeSessionStarted, protocol.TypeStdout, protocol.TypeStderr, protocol.TypeExit, protocol.TypeError:
	default:
		return fmt.Errorf("runner: unexpected message type %s", msg.Type)
	}
	if msg.SessionID == "" {
		return fmt.Errorf("runner: session-scoped message missing session id")
	}
	pending := c.pending[msg.SessionID]
	if pending == nil {
		if len(c.pending) >= maxPendingSessions {
			return fmt.Errorf("runner: too many unattached sessions buffered")
		}
		pending = &pendingSessionMessages{}
		c.pending[msg.SessionID] = pending
	}
	size := len(msg.Payload) + len(msg.SessionID) + 64
	if len(pending.messages) >= maxPendingSessionMessages || pending.bytes+size > maxPendingSessionBytes {
		return fmt.Errorf("runner: pending messages exceed reconnect buffer for session %s", msg.SessionID)
	}
	pending.messages = append(pending.messages, msg)
	pending.bytes += size
	return nil
}

func deliverSessionMessage(session *Session, msg protocol.Message) (bool, error) {
	switch msg.Type {
	case protocol.TypeSessionStarted:
		session.markStarted(nil)
		return false, nil
	case protocol.TypeStdout, protocol.TypeStderr:
		stream, err := protocol.DecodePayload[protocol.StreamData](msg)
		if err != nil {
			return false, err
		}
		if msg.Type == protocol.TypeStdout {
			session.stdout.push(stream.Data)
		} else {
			session.stderr.push(stream.Data)
		}
		return false, nil
	case protocol.TypeExit:
		exit, err := protocol.DecodePayload[protocol.ExitResult](msg)
		if err != nil {
			return true, err
		}
		session.finish(Result{ExitCode: exit.ExitCode, Signaled: exit.Signaled}, nil)
		return true, nil
	case protocol.TypeError:
		err := protocolErrorFromMessage(msg)
		session.fail(err)
		return true, nil
	default:
		return false, fmt.Errorf("runner: unexpected message type %s", msg.Type)
	}
}

func protocolErrorFromMessage(msg protocol.Message) error {
	payload, err := protocol.DecodePayload[protocol.ErrorPayload](msg)
	if err != nil {
		return fmt.Errorf("runner: invalid error payload: %w", err)
	}
	return &ProtocolError{Code: payload.Code, Message: payload.Message}
}

func (c *Connection) readProtocolMessage() (protocol.Message, error) {
	messageType, data, err := c.conn.ReadMessage()
	if err != nil {
		return protocol.Message{}, err
	}
	if messageType != websocket.TextMessage {
		return protocol.Message{}, fmt.Errorf("runner: protocol frame must be text")
	}
	return protocol.Decode(data)
}

func (c *Connection) write(typ protocol.MessageType, sessionID string, payload any) error {
	msg, err := protocol.NewMessage(protocol.Version1, typ, sessionID, payload)
	if err != nil {
		return err
	}
	data, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Connection) fail(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = ErrClosed
		}
		c.mu.Lock()
		c.err = err
		sessions := make([]*Session, 0, len(c.sessions))
		for _, session := range c.sessions {
			sessions = append(sessions, session)
		}
		c.sessions = make(map[string]*Session)
		c.pending = make(map[string]*pendingSessionMessages)
		c.mu.Unlock()
		for _, session := range sessions {
			session.fail(err)
		}
		_ = c.conn.Close()
		close(c.done)
	})
}

func (c *Connection) connectionError() error {
	if err := c.Err(); err != nil {
		return err
	}
	return ErrDisconnected
}

func (c *Connection) Close() error {
	c.fail(ErrClosed)
	return nil
}
