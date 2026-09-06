package protocol

import runnerprotocol "github.com/brantje/agent-board/packages/runnerprotocol"

func Encode(msg Message) ([]byte, error) {
	return runnerprotocol.Encode(msg)
}

func Decode(data []byte) (Message, error) {
	return runnerprotocol.Decode(data)
}
