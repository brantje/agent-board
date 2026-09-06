package protocol

import runnerprotocol "github.com/brantje/agent-board/packages/runnerprotocol"

const Version1 = runnerprotocol.Version1

type MessageType = runnerprotocol.MessageType

const (
	TypeServerHello    = runnerprotocol.TypeServerHello
	TypeRunnerHello    = runnerprotocol.TypeRunnerHello
	TypeHealth         = runnerprotocol.TypeHealth
	TypeStart          = runnerprotocol.TypeStart
	TypeSessionStarted = runnerprotocol.TypeSessionStarted
	TypeStdin          = runnerprotocol.TypeStdin
	TypeStdinClose     = runnerprotocol.TypeStdinClose
	TypeStdout         = runnerprotocol.TypeStdout
	TypeStderr         = runnerprotocol.TypeStderr
	TypeExit           = runnerprotocol.TypeExit
	TypeTerminate      = runnerprotocol.TypeTerminate
	TypeKill           = runnerprotocol.TypeKill
	TypeError          = runnerprotocol.TypeError
)

var (
	ErrInvalidMessage     = runnerprotocol.ErrInvalidMessage
	ErrUnsupportedVersion = runnerprotocol.ErrUnsupportedVersion
)

type Message = runnerprotocol.Message
type ServerHello = runnerprotocol.ServerHello
type RunnerHello = runnerprotocol.RunnerHello
type Capabilities = runnerprotocol.Capabilities
type Health = runnerprotocol.Health
type StartRequest = runnerprotocol.StartRequest
type StreamData = runnerprotocol.StreamData
type ExitResult = runnerprotocol.ExitResult
type ErrorPayload = runnerprotocol.ErrorPayload

func NewMessage(version int, typ MessageType, sessionID string, payload any) (Message, error) {
	return runnerprotocol.NewMessage(version, typ, sessionID, payload)
}

func DecodePayload[T any](m Message) (T, error) {
	return runnerprotocol.DecodePayload[T](m)
}
