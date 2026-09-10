package protocol

import runnerprotocol "github.com/brantje/agent-board/packages/runnerprotocol"

const Version2 = runnerprotocol.Version2

type MessageType = runnerprotocol.MessageType

const (
	TypeServerHello      = runnerprotocol.TypeServerHello
	TypeRunnerHello      = runnerprotocol.TypeRunnerHello
	TypeHealth           = runnerprotocol.TypeHealth
	TypeStart            = runnerprotocol.TypeStart
	TypeSessionStarted   = runnerprotocol.TypeSessionStarted
	TypeStdin            = runnerprotocol.TypeStdin
	TypeStdinClose       = runnerprotocol.TypeStdinClose
	TypeStdout           = runnerprotocol.TypeStdout
	TypeStderr           = runnerprotocol.TypeStderr
	TypeExit             = runnerprotocol.TypeExit
	TypeTerminate        = runnerprotocol.TypeTerminate
	TypeKill             = runnerprotocol.TypeKill
	TypeConnect          = runnerprotocol.TypeConnect
	TypeConnected        = runnerprotocol.TypeConnected
	TypeConnectData      = runnerprotocol.TypeConnectData
	TypeConnectClose     = runnerprotocol.TypeConnectClose
	TypeTransferBegin    = runnerprotocol.TypeTransferBegin
	TypeTransferChunk    = runnerprotocol.TypeTransferChunk
	TypeTransferEnd      = runnerprotocol.TypeTransferEnd
	TypeTransferFailed   = runnerprotocol.TypeTransferFailed
	TypeTransferApplied  = runnerprotocol.TypeTransferApplied
	TypeError            = runnerprotocol.TypeError
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
type ConnectRequest = runnerprotocol.ConnectRequest
type Connected = runnerprotocol.Connected
type ConnectData = runnerprotocol.ConnectData
type ConnectClose = runnerprotocol.ConnectClose
type ExitResult = runnerprotocol.ExitResult
type ErrorPayload = runnerprotocol.ErrorPayload
type TransferBegin = runnerprotocol.TransferBegin
type TransferChunk = runnerprotocol.TransferChunk
type TransferEnd = runnerprotocol.TransferEnd
type TransferFailed = runnerprotocol.TransferFailed
type TransferApplied = runnerprotocol.TransferApplied

const (
	TransferChunkSize = runnerprotocol.TransferChunkSize
	MaxTransferBytes  = runnerprotocol.MaxTransferBytes
	MaxMessageSize    = runnerprotocol.MaxMessageSize
)

func ValidateTransferBegin(begin TransferBegin) error {
	return runnerprotocol.ValidateTransferBegin(begin)
}

func NewMessage(version int, typ MessageType, sessionID string, payload any) (Message, error) {
	return runnerprotocol.NewMessage(version, typ, sessionID, payload)
}

func DecodePayload[T any](m Message) (T, error) {
	return runnerprotocol.DecodePayload[T](m)
}
