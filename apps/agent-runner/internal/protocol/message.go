package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

const Version1 = 1

type MessageType string

const (
	TypeServerHello    MessageType = "server_hello"
	TypeRunnerHello    MessageType = "runner_hello"
	TypeHealth         MessageType = "health"
	TypeStart          MessageType = "start"
	TypeSessionStarted MessageType = "session_started"
	TypeStdin          MessageType = "stdin"
	TypeStdinClose     MessageType = "stdin_close"
	TypeStdout         MessageType = "stdout"
	TypeStderr         MessageType = "stderr"
	TypeExit           MessageType = "exit"
	TypeTerminate      MessageType = "terminate"
	TypeKill           MessageType = "kill"
	TypeError          MessageType = "error"
)

var (
	ErrInvalidMessage     = errors.New("invalid protocol message")
	ErrUnsupportedVersion = errors.New("unsupported protocol version")
)

type Message struct {
	Version   int             `json:"version"`
	Type      MessageType     `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type ServerHello struct {
	SupportedVersions []int `json:"supported_versions"`
}

type RunnerHello struct {
	Version      int          `json:"version"`
	Capabilities Capabilities `json:"capabilities"`
}

type Capabilities struct {
	MaxActiveSessions int      `json:"max_active_sessions"`
	Features          []string `json:"features"`
}

type Health struct {
	Status           string   `json:"status"`
	ActiveSessions   int      `json:"active_sessions"`
	ActiveSessionIDs []string `json:"active_session_ids,omitempty"`
}

type StartRequest struct {
	Command []string          `json:"command"`
	Dir     string            `json:"dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

type StreamData struct {
	Data []byte `json:"data"`
}

type ExitResult struct {
	ExitCode int  `json:"exit_code"`
	Signaled bool `json:"signaled,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewMessage(version int, typ MessageType, sessionID string, payload any) (Message, error) {
	var raw json.RawMessage
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return Message{}, fmt.Errorf("marshal protocol payload: %w", err)
		}
		raw = encoded
	}

	msg := Message{Version: version, Type: typ, SessionID: sessionID, Payload: raw}
	if err := msg.Validate(); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func (m Message) Validate() error {
	if m.Version != Version1 {
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, m.Version)
	}
	if !knownType(m.Type) {
		return fmt.Errorf("%w: unknown message type %q", ErrInvalidMessage, m.Type)
	}
	if requiresSession(m.Type) && m.SessionID == "" {
		return fmt.Errorf("%w: %s requires session_id", ErrInvalidMessage, m.Type)
	}
	if forbidsSession(m.Type) && m.SessionID != "" {
		return fmt.Errorf("%w: %s must not include session_id", ErrInvalidMessage, m.Type)
	}
	if len(m.Payload) > 0 && !json.Valid(m.Payload) {
		return fmt.Errorf("%w: payload is not valid JSON", ErrInvalidMessage)
	}
	return nil
}

func DecodePayload[T any](m Message) (T, error) {
	var value T
	if len(m.Payload) == 0 {
		return value, fmt.Errorf("%w: %s requires payload", ErrInvalidMessage, m.Type)
	}
	if err := json.Unmarshal(m.Payload, &value); err != nil {
		return value, fmt.Errorf("decode %s payload: %w", m.Type, err)
	}
	return value, nil
}

func knownType(typ MessageType) bool {
	switch typ {
	case TypeServerHello, TypeRunnerHello, TypeHealth, TypeStart, TypeSessionStarted,
		TypeStdin, TypeStdinClose, TypeStdout, TypeStderr, TypeExit,
		TypeTerminate, TypeKill, TypeError:
		return true
	default:
		return false
	}
}

func requiresSession(typ MessageType) bool {
	switch typ {
	case TypeStart, TypeSessionStarted, TypeStdin, TypeStdinClose, TypeStdout,
		TypeStderr, TypeExit, TypeTerminate, TypeKill:
		return true
	default:
		return false
	}
}

func forbidsSession(typ MessageType) bool {
	switch typ {
	case TypeServerHello, TypeRunnerHello, TypeHealth:
		return true
	default:
		return false
	}
}
