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
	maxMessageSize = 1 << 20
	writeTimeout   = 10 * time.Second
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

type Connection struct {
	conn *websocket.Conn

	writeMu   sync.Mutex
	mu        sync.RWMutex
	sessions  map[string]*Session
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
	c := &Connection{conn: conn, sessions: make(map[string]*Session), done: make(chan struct{})}
	conn.SetReadLimit(maxMessageSize)
	if err := c.handshake(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	go c.readLoop()
	return c, nil
}

func (c *Connection) handshake() error {
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

func validateFeatures(features []string) error {
	required := map[string]bool{"stdin": false, "stdout": false, "stderr": false, "terminate": false, "kill": false, "health": false}
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
	if err := c.write(protocol.TypeStart, sessionID, protocol.StartRequest{Command: request.Command, Dir: request.Dir, Env: request.Env, Secrets: request.Secrets}); err != nil {
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
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.connectionError()
	}
}

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
	c.mu.RLock()
	session := c.sessions[msg.SessionID]
	c.mu.RUnlock()
	if session == nil {
		return nil
	}
	switch msg.Type {
	case protocol.TypeSessionStarted:
		session.markStarted(nil)
	case protocol.TypeStdout, protocol.TypeStderr:
		stream, err := protocol.DecodePayload[protocol.StreamData](msg)
		if err != nil {
			session.fail(err)
			c.unregister(msg.SessionID, session)
			return nil
		}
		if msg.Type == protocol.TypeStdout {
			session.stdout.push(stream.Data)
		} else {
			session.stderr.push(stream.Data)
		}
	case protocol.TypeExit:
		exit, err := protocol.DecodePayload[protocol.ExitResult](msg)
		if err != nil {
			session.fail(err)
		} else {
			session.finish(Result{ExitCode: exit.ExitCode, Signaled: exit.Signaled}, nil)
		}
		c.unregister(msg.SessionID, session)
	case protocol.TypeError:
		err := protocolErrorFromMessage(msg)
		session.fail(err)
		c.unregister(msg.SessionID, session)
	default:
		return fmt.Errorf("runner: unexpected message type %s", msg.Type)
	}
	return nil
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
