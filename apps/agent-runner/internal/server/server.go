package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/brantje/agent-board/apps/agent-runner/internal/session"
	"github.com/gorilla/websocket"
)

const maxMessageSize = 1 << 20

type Config struct {
	WorkspaceRoot     string
	MaxActiveSessions int
}

type Server struct {
	manager  *session.Manager
	upgrader websocket.Upgrader
	mux      *http.ServeMux
}

func New(config Config) *Server {
	manager := session.NewManagerWithWorkspace(config.MaxActiveSessions, config.WorkspaceRoot)
	s := &Server{
		manager: manager,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
		},
		mux: http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /v1/ws", s.handleWebSocket)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.health())
}

func (s *Server) health() protocol.Health {
	ids := s.manager.ActiveIDs()
	return protocol.Health{Status: "ok", ActiveSessions: len(ids), ActiveSessionIDs: ids}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxMessageSize)
	writer := &connectionWriter{conn: conn}

	if !s.handshake(conn, writer) {
		return
	}
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			writer.sendError("invalid_message", "protocol messages must be text", "")
			continue
		}
		msg, err := protocol.Decode(data)
		if err != nil {
			if errors.Is(err, protocol.ErrUnsupportedVersion) {
				writer.sendError("unsupported_protocol_version", "protocol version is not supported", "")
				return
			}
			writer.sendError("invalid_message", "invalid protocol message", "")
			continue
		}
		s.handleMessage(writer, msg)
	}
}

func (s *Server) handshake(conn *websocket.Conn, writer *connectionWriter) bool {
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		return false
	}
	if messageType != websocket.TextMessage {
		writer.sendError("invalid_handshake", "expected server hello", "")
		return false
	}
	msg, err := protocol.Decode(data)
	if err != nil {
		code := "invalid_handshake"
		message := "invalid server hello"
		if errors.Is(err, protocol.ErrUnsupportedVersion) {
			code = "unsupported_protocol_version"
			message = "protocol version is not supported"
		}
		writer.sendError(code, message, "")
		return false
	}
	if msg.Type != protocol.TypeServerHello {
		writer.sendError("invalid_handshake", "expected server hello", "")
		return false
	}
	hello, err := protocol.DecodePayload[protocol.ServerHello](msg)
	if err != nil || !containsVersion(hello.SupportedVersions, protocol.Version1) {
		writer.sendError("unsupported_protocol_version", "protocol version is not supported", "")
		return false
	}

	_ = writer.send(protocol.TypeRunnerHello, "", protocol.RunnerHello{
		Version: protocol.Version1,
		Capabilities: protocol.Capabilities{
			MaxActiveSessions: s.manager.Capacity(),
			Features:          []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"},
		},
	})
	_ = writer.send(protocol.TypeHealth, "", s.health())
	return true
}

func (s *Server) handleMessage(writer *connectionWriter, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeStart:
		s.handleStart(writer, msg)
	case protocol.TypeStdin:
		s.handleStdin(writer, msg)
	case protocol.TypeStdinClose:
		s.handleStdinClose(writer, msg)
	case protocol.TypeTerminate:
		s.handleSignal(writer, msg.SessionID, false)
	case protocol.TypeKill:
		s.handleSignal(writer, msg.SessionID, true)
	case protocol.TypeHealth:
		_ = writer.send(protocol.TypeHealth, "", s.health())
	default:
		writer.sendError("invalid_direction", "message type is not accepted from server", msg.SessionID)
	}
}

func (s *Server) handleStart(writer *connectionWriter, msg protocol.Message) {
	request, err := protocol.DecodePayload[protocol.StartRequest](msg)
	if err != nil {
		writer.sendError("invalid_start", "invalid start request", msg.SessionID)
		return
	}
	execution, err := s.manager.Start(msg.SessionID, session.Request{
		Command: request.Command,
		Dir: request.Dir,
		Env: request.Env,
		Secrets: request.Secrets,
	})
	if err != nil {
		switch {
		case errors.Is(err, session.ErrCapacityReached):
			writer.sendError("capacity_reached", "runner session capacity reached", msg.SessionID)
		case errors.Is(err, session.ErrDuplicateID):
			writer.sendError("duplicate_session", "execution session already exists", msg.SessionID)
		default:
			writer.sendError("start_failed", "execution session could not be started", msg.SessionID)
		}
		return
	}

	_ = writer.send(protocol.TypeSessionStarted, msg.SessionID, nil)
	go streamExecution(writer, execution)
}

func (s *Server) handleStdin(writer *connectionWriter, msg protocol.Message) {
	execution, err := s.manager.Get(msg.SessionID)
	if err != nil {
		writer.sendError("session_not_found", "execution session is not active", msg.SessionID)
		return
	}
	stream, err := protocol.DecodePayload[protocol.StreamData](msg)
	if err != nil {
		writer.sendError("invalid_stdin", "invalid stdin payload", msg.SessionID)
		return
	}
	if _, err := execution.Stdin().Write(stream.Data); err != nil {
		writer.sendError("stdin_failed", "stdin is closed", msg.SessionID)
	}
}

func (s *Server) handleStdinClose(writer *connectionWriter, msg protocol.Message) {
	execution, err := s.manager.Get(msg.SessionID)
	if err != nil {
		writer.sendError("session_not_found", "execution session is not active", msg.SessionID)
		return
	}
	if err := execution.Stdin().Close(); err != nil {
		writer.sendError("stdin_close_failed", "stdin could not be closed", msg.SessionID)
	}
}

func (s *Server) handleSignal(writer *connectionWriter, sessionID string, force bool) {
	execution, err := s.manager.Get(sessionID)
	if err != nil {
		writer.sendError("session_not_found", "execution session is not active", sessionID)
		return
	}
	if force {
		err = execution.Kill()
	} else {
		err = execution.Terminate()
	}
	if err != nil {
		writer.sendError("signal_failed", "execution session could not be signaled", sessionID)
	}
}

func streamExecution(writer *connectionWriter, execution *session.Session) {
	var streams sync.WaitGroup
	streams.Add(2)
	go func() {
		defer streams.Done()
		pumpStream(writer, protocol.TypeStdout, execution.ID(), execution.Stdout())
	}()
	go func() {
		defer streams.Done()
		pumpStream(writer, protocol.TypeStderr, execution.ID(), execution.Stderr())
	}()

	result, err := execution.Wait(context.Background())
	streams.Wait()
	if err != nil {
		writer.sendError("wait_failed", "execution result is unavailable", execution.ID())
		return
	}
	_ = writer.send(protocol.TypeExit, execution.ID(), protocol.ExitResult{ExitCode: result.ExitCode, Signaled: result.Signaled})
}

func pumpStream(writer *connectionWriter, typ protocol.MessageType, sessionID string, reader io.Reader) {
	buffer := make([]byte, 32*1024)
	sendOutput := true
	for {
		n, err := reader.Read(buffer)
		if n > 0 && sendOutput {
			chunk := append([]byte(nil), buffer[:n]...)
			if writer.send(typ, sessionID, protocol.StreamData{Data: chunk}) != nil {
				sendOutput = false
			}
		}
		if err != nil {
			return
		}
	}
}

func containsVersion(versions []int, target int) bool {
	for _, version := range versions {
		if version == target {
			return true
		}
	}
	return false
}

type connectionWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *connectionWriter) send(typ protocol.MessageType, sessionID string, payload any) error {
	msg, err := protocol.NewMessage(protocol.Version1, typ, sessionID, payload)
	if err != nil {
		return err
	}
	data, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.TextMessage, data)
}

func (w *connectionWriter) sendError(code, message, sessionID string) {
	_ = w.send(protocol.TypeError, sessionID, protocol.ErrorPayload{Code: code, Message: message})
}
