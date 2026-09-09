package runner

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func (c *Connection) SendTransfer(ctx context.Context, sessionID, transferID, direction string, payload []byte) error {
	if c == nil || sessionID == "" || transferID == "" {
		return fmt.Errorf("runner transfer requires session and transfer ids")
	}
	checksum := sha256.Sum256(payload)
	if err := c.write(protocol.TypeTransferBegin, sessionID, protocol.TransferBegin{
		TransferID: transferID,
		Direction:  direction,
		TotalBytes: int64(len(payload)),
		Checksum:   hex.EncodeToString(checksum[:]),
	}); err != nil {
		return err
	}
	chunkSize := protocol.TransferChunkSize
	for offset := 0; offset < len(payload); offset += chunkSize {
		end := offset + chunkSize
		if end > len(payload) {
			end = len(payload)
		}
		if err := c.write(protocol.TypeTransferChunk, sessionID, protocol.TransferChunk{
			TransferID: transferID,
			Data:       base64.StdEncoding.EncodeToString(payload[offset:end]),
		}); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return c.write(protocol.TypeTransferEnd, sessionID, protocol.TransferEnd{TransferID: transferID})
}

func (c *Connection) ReceiveTransfer(ctx context.Context, sessionID string) (transferID string, payload []byte, err error) {
	if c == nil || sessionID == "" {
		return "", nil, fmt.Errorf("runner transfer requires session id")
	}
	var (
		expected int64
		checksum string
		buffer   []byte
	)
	for {
		msg, err := c.readProtocolMessage()
		if err != nil {
			return "", nil, err
		}
		if msg.SessionID != sessionID {
			continue
		}
		switch msg.Type {
		case protocol.TypeTransferBegin:
			begin, err := protocol.DecodePayload[protocol.TransferBegin](msg)
			if err != nil {
				return "", nil, err
			}
			transferID = begin.TransferID
			expected = begin.TotalBytes
			checksum = begin.Checksum
			buffer = make([]byte, 0, expected)
		case protocol.TypeTransferChunk:
			chunk, err := protocol.DecodePayload[protocol.TransferChunk](msg)
			if err != nil {
				return "", nil, err
			}
			if transferID != "" && chunk.TransferID != transferID {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(chunk.Data)
			if err != nil {
				return "", nil, fmt.Errorf("decode transfer chunk: %w", err)
			}
			buffer = append(buffer, data...)
			if expected > 0 && int64(len(buffer)) > expected {
				return "", nil, fmt.Errorf("transfer payload exceeded declared size")
			}
		case protocol.TypeTransferEnd:
			end, err := protocol.DecodePayload[protocol.TransferEnd](msg)
			if err != nil {
				return "", nil, err
			}
			if transferID != "" && end.TransferID != transferID {
				continue
			}
			sum := sha256.Sum256(buffer)
			if checksum != "" && hex.EncodeToString(sum[:]) != checksum {
				return "", nil, fmt.Errorf("transfer checksum mismatch")
			}
			if expected > 0 && int64(len(buffer)) != expected {
				return "", nil, fmt.Errorf("transfer payload size mismatch")
			}
			return transferID, buffer, nil
		case protocol.TypeTransferFailed:
			failed, err := protocol.DecodePayload[protocol.TransferFailed](msg)
			if err != nil {
				return "", nil, err
			}
			return "", nil, fmt.Errorf("runner transfer failed: %s", failed.Message)
		case protocol.TypeError:
			return "", nil, protocolErrorFromMessage(msg)
		default:
			if err := c.handleMessage(msg); err != nil {
				return "", nil, err
			}
		}
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
	}
}
