package runnerprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const Version2 = 2

const RunnerIDHeader = "X-Agent-Board-Runner-Id"

type MessageType string

const (
	TypeServerHello               MessageType = "server_hello"
	TypeRunnerHello               MessageType = "runner_hello"
	TypeHealth                    MessageType = "health"
	TypeStart                     MessageType = "start"
	TypeSessionStarted            MessageType = "session_started"
	TypeStdin                     MessageType = "stdin"
	TypeStdinClose                MessageType = "stdin_close"
	TypeStdout                    MessageType = "stdout"
	TypeStderr                    MessageType = "stderr"
	TypeExit                      MessageType = "exit"
	TypeTerminate                 MessageType = "terminate"
	TypeKill                      MessageType = "kill"
	TypeConnect                   MessageType = "connect"
	TypeConnected                 MessageType = "connected"
	TypeConnectData               MessageType = "connect_data"
	TypeConnectClose              MessageType = "connect_close"
	TypeTransferBegin             MessageType = "transfer_begin"
	TypeTransferChunk             MessageType = "transfer_chunk"
	TypeTransferEnd               MessageType = "transfer_end"
	TypeTransferFailed            MessageType = "transfer_failed"
	TypeTransferApplied           MessageType = "transfer_applied"
	TypeWorkspacePublish          MessageType = "workspace_publish"
	TypeWorkspacePublished        MessageType = "workspace_published"
	TypeWorkspacePublishApplied   MessageType = "workspace_publish_applied"
	TypeError                     MessageType = "error"
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
	RunnerVersion     string   `json:"runner_version"`
	OS                string   `json:"os"`
	Architecture      string   `json:"architecture"`
	Engines           []string `json:"engines"`
	MaxActiveSessions int      `json:"max_active_sessions"`
	Features          []string `json:"features"`
}

type Health struct {
	Status           string   `json:"status"`
	ActiveSessions   int      `json:"active_sessions"`
	ActiveSessionIDs []string `json:"active_session_ids,omitempty"`
}

type GitWorkspace struct {
	CloneURL         string `json:"clone_url"`
	Ref              string `json:"ref,omitempty"`
	IssueBranch      string `json:"issue_branch"`
	RecordedRevision string `json:"recorded_revision,omitempty"`
}

type StartRequest struct {
	Command   []string          `json:"command"`
	Dir       string            `json:"dir,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Secrets   map[string]string `json:"secrets,omitempty"`
	Workspace *GitWorkspace     `json:"workspace,omitempty"`
}

type WorkspacePublished struct {
	Revision string `json:"revision"`
}

type WorkspacePublishApplied struct {
	Revision string `json:"revision"`
}

type StreamData struct {
	Data []byte `json:"data"`
}

// ConnectRequest opens a session-scoped connection from agent-runner to a
// service that is local to the Runtime. The runner applies its own destination
// policy; v0.1 only permits loopback TCP destinations.
type ConnectRequest struct {
	ConnectionID string `json:"connection_id"`
	Network      string `json:"network"`
	Address      string `json:"address"`
}

type Connected struct {
	ConnectionID string `json:"connection_id"`
}

type ConnectData struct {
	ConnectionID string `json:"connection_id"`
	Data         []byte `json:"data"`
}

type ConnectClose struct {
	ConnectionID string `json:"connection_id"`
	Code         string `json:"code,omitempty"`
	Message      string `json:"message,omitempty"`
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
	if m.Version != Version2 {
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
	trimmed := bytes.TrimSpace(m.Payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return value, fmt.Errorf("%w: %s payload must be a JSON object", ErrInvalidMessage, m.Type)
	}
	decoder := json.NewDecoder(bytes.NewReader(m.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode %s payload: %w", m.Type, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return value, fmt.Errorf("decode %s payload: %w", m.Type, err)
	}
	return value, nil
}

func knownType(typ MessageType) bool {
	switch typ {
	case TypeServerHello, TypeRunnerHello, TypeHealth, TypeStart, TypeSessionStarted,
		TypeStdin, TypeStdinClose, TypeStdout, TypeStderr, TypeExit,
		TypeTerminate, TypeKill, TypeConnect, TypeConnected, TypeConnectData,
		TypeConnectClose, TypeTransferBegin, TypeTransferChunk, TypeTransferEnd,
		TypeTransferFailed, TypeTransferApplied, TypeWorkspacePublish,
		TypeWorkspacePublished, TypeWorkspacePublishApplied, TypeError:
		return true
	default:
		return false
	}
}

func requiresSession(typ MessageType) bool {
	switch typ {
	case TypeStart, TypeSessionStarted, TypeStdin, TypeStdinClose, TypeStdout,
		TypeStderr, TypeExit, TypeTerminate, TypeKill, TypeConnect, TypeConnected,
		TypeConnectData, TypeConnectClose, TypeTransferBegin, TypeTransferChunk,
		TypeTransferEnd, TypeTransferFailed, TypeTransferApplied,
		TypeWorkspacePublish, TypeWorkspacePublished, TypeWorkspacePublishApplied:
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
