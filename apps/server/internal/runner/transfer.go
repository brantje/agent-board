package runner

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func (c *Connection) SendTransfer(ctx context.Context, sessionID, transferID, direction string, payload []byte, onProgress TransferProgressFunc) error {
	if c == nil || sessionID == "" || transferID == "" {
		return fmt.Errorf("runner transfer requires session and transfer ids")
	}
	total := int64(len(payload))
	checksum := sha256.Sum256(payload)
	if err := c.write(protocol.TypeTransferBegin, sessionID, protocol.TransferBegin{
		TransferID: transferID,
		Direction:  direction,
		TotalBytes: total,
		Checksum:   hex.EncodeToString(checksum[:]),
	}); err != nil {
		return err
	}
	var progressState transferProgressState
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
		emitTransferProgress(onProgress, int64(end), total, &progressState, time.Now(), DefaultTransferProgressInterval)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	emitTransferProgress(onProgress, total, total, &progressState, time.Now(), DefaultTransferProgressInterval)
	return c.write(protocol.TypeTransferEnd, sessionID, protocol.TransferEnd{TransferID: transferID})
}

// AcknowledgeTransferApplied is sent only after the server has verified and
// successfully applied a from_runner bundle. It is the Runner's cleanup boundary.
func (c *Connection) AcknowledgeTransferApplied(ctx context.Context, sessionID, transferID string) error {
	if c == nil || sessionID == "" || transferID == "" {
		return fmt.Errorf("runner transfer acknowledgement requires session and transfer ids")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.write(protocol.TypeTransferApplied, sessionID, protocol.TransferApplied{TransferID: transferID})
}

func (c *Connection) ReceiveTransfer(ctx context.Context, sessionID string, onProgress TransferProgressFunc) (transferID string, payload []byte, err error) {
	if c == nil || sessionID == "" {
		return "", nil, fmt.Errorf("runner transfer requires session id")
	}
	c.mu.Lock()
	if result, ok := c.transferDone[sessionID]; ok {
		delete(c.transferDone, sessionID)
		c.mu.Unlock()
		return result.transferID, result.payload, result.err
	}
	waiter := &transferWaiter{result: make(chan transferResult, 1), onProgress: onProgress}
	c.transferWaiters[sessionID] = waiter
	c.mu.Unlock()

	select {
	case result := <-waiter.result:
		return result.transferID, result.payload, result.err
	case <-ctx.Done():
		c.mu.Lock()
		if c.transferWaiters[sessionID] == waiter {
			delete(c.transferWaiters, sessionID)
		}
		c.mu.Unlock()
		return "", nil, ctx.Err()
	case <-c.done:
		return "", nil, c.connectionError()
	}
}

func (c *Connection) handleTransferMessage(msg protocol.Message) error {
	if msg.SessionID == "" {
		return fmt.Errorf("runner: transfer message missing session id")
	}
	switch msg.Type {
	case protocol.TypeTransferBegin:
		begin, err := protocol.DecodePayload[protocol.TransferBegin](msg)
		if err != nil {
			return err
		}
		if err := protocol.ValidateTransferBegin(begin); err != nil {
			return err
		}
		c.mu.Lock()
		c.transfers[msg.SessionID] = &incomingTransferState{
			transferID: begin.TransferID,
			expected:   begin.TotalBytes,
			checksum:   begin.Checksum,
			buffer:     make([]byte, 0, begin.TotalBytes),
		}
		c.mu.Unlock()
		return nil
	case protocol.TypeTransferChunk:
		chunk, err := protocol.DecodePayload[protocol.TransferChunk](msg)
		if err != nil {
			return err
		}
		c.mu.Lock()
		transfer := c.transfers[msg.SessionID]
		if transfer == nil {
			c.mu.Unlock()
			return nil
		}
		if transfer.transferID != "" && chunk.TransferID != transfer.transferID {
			c.mu.Unlock()
			return nil
		}
		data, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil {
			c.mu.Unlock()
			return fmt.Errorf("decode transfer chunk: %w", err)
		}
		if transfer.expected == 0 && len(data) > 0 {
			c.mu.Unlock()
			return c.completeTransfer(msg.SessionID, transferResult{err: fmt.Errorf("transfer payload exceeded declared size")})
		}
		if int64(len(transfer.buffer)+len(data)) > protocol.MaxTransferBytes {
			c.mu.Unlock()
			return c.completeTransfer(msg.SessionID, transferResult{err: fmt.Errorf("transfer payload exceeded declared size")})
		}
		transfer.buffer = append(transfer.buffer, data...)
		transferred := int64(len(transfer.buffer))
		waiter := c.transferWaiters[msg.SessionID]
		if transfer.expected > 0 && transferred > transfer.expected {
			c.mu.Unlock()
			return c.completeTransfer(msg.SessionID, transferResult{err: fmt.Errorf("transfer payload exceeded declared size")})
		}
		if waiter != nil {
			emitTransferProgress(waiter.onProgress, transferred, transfer.expected, &transfer.progressState, time.Now(), DefaultTransferProgressInterval)
		}
		c.mu.Unlock()
		return nil
	case protocol.TypeTransferEnd:
		end, err := protocol.DecodePayload[protocol.TransferEnd](msg)
		if err != nil {
			return err
		}
		c.mu.Lock()
		transfer := c.transfers[msg.SessionID]
		if transfer == nil {
			c.mu.Unlock()
			return nil
		}
		if transfer.transferID != "" && end.TransferID != transfer.transferID {
			c.mu.Unlock()
			return nil
		}
		payload := append([]byte(nil), transfer.buffer...)
		transferID := transfer.transferID
		checksum := transfer.checksum
		expected := transfer.expected
		delete(c.transfers, msg.SessionID)
		c.mu.Unlock()

		sum := sha256.Sum256(payload)
		if checksum != "" && hex.EncodeToString(sum[:]) != checksum {
			return c.completeTransfer(msg.SessionID, transferResult{err: fmt.Errorf("transfer checksum mismatch")})
		}
		if expected > 0 && int64(len(payload)) != expected {
			return c.completeTransfer(msg.SessionID, transferResult{err: fmt.Errorf("transfer payload size mismatch")})
		}
		return c.completeTransfer(msg.SessionID, transferResult{transferID: transferID, payload: payload})
	case protocol.TypeTransferFailed:
		failed, err := protocol.DecodePayload[protocol.TransferFailed](msg)
		if err != nil {
			return err
		}
		return c.completeTransfer(msg.SessionID, transferResult{err: fmt.Errorf("runner transfer failed: %s", failed.Message)})
	default:
		return fmt.Errorf("runner: unexpected transfer message type %s", msg.Type)
	}
}

func (c *Connection) completeTransfer(sessionID string, result transferResult) error {
	c.mu.Lock()
	waiter := c.transferWaiters[sessionID]
	if waiter != nil {
		delete(c.transferWaiters, sessionID)
	}
	c.transferDone[sessionID] = result
	c.mu.Unlock()
	if waiter != nil {
		waiter.result <- result
	}
	return result.err
}
