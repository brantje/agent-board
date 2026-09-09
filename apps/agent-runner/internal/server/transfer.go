package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/brantje/agent-board/apps/agent-runner/internal/workspace"
)

type incomingTransfer struct {
	transferID string
	direction  string
	expected   int64
	checksum   string
	buffer     []byte
}

type transferState struct {
	mu         sync.Mutex
	incoming   map[string]*incomingTransfer
	ready      map[string]bool
}

func newTransferState() *transferState {
	return &transferState{
		incoming: make(map[string]*incomingTransfer),
		ready:    make(map[string]bool),
	}
}

func (s *transferState) begin(sessionID string, begin protocol.TransferBegin) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(begin.TransferID) == "" {
		return errors.New("transfer requires session and transfer ids")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incoming[sessionID] = &incomingTransfer{
		transferID: begin.TransferID,
		direction:  begin.Direction,
		expected:   begin.TotalBytes,
		checksum:   begin.Checksum,
		buffer:     make([]byte, 0, begin.TotalBytes),
	}
	return nil
}

func (s *transferState) chunk(sessionID string, chunk protocol.TransferChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer := s.incoming[sessionID]
	if transfer == nil {
		return errors.New("transfer is not active")
	}
	if transfer.transferID != "" && chunk.TransferID != transfer.transferID {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		return fmt.Errorf("decode transfer chunk: %w", err)
	}
	transfer.buffer = append(transfer.buffer, data...)
	if transfer.expected > 0 && int64(len(transfer.buffer)) > transfer.expected {
		return errors.New("transfer payload exceeded declared size")
	}
	return nil
}

func (s *transferState) end(sessionID string, end protocol.TransferEnd) ([]byte, string, error) {
	s.mu.Lock()
	transfer := s.incoming[sessionID]
	if transfer == nil {
		s.mu.Unlock()
		return nil, "", errors.New("transfer is not active")
	}
	if transfer.transferID != "" && end.TransferID != transfer.transferID {
		s.mu.Unlock()
		return nil, "", nil
	}
	payload := append([]byte(nil), transfer.buffer...)
	direction := transfer.direction
	delete(s.incoming, sessionID)
	s.mu.Unlock()

	sum := sha256.Sum256(payload)
	if transfer.checksum != "" && hex.EncodeToString(sum[:]) != transfer.checksum {
		return nil, "", errors.New("transfer checksum mismatch")
	}
	if transfer.expected > 0 && int64(len(payload)) != transfer.expected {
		return nil, "", errors.New("transfer payload size mismatch")
	}
	return payload, direction, nil
}

func (s *transferState) markReady(sessionID string) {
	s.mu.Lock()
	s.ready[sessionID] = true
	s.mu.Unlock()
}

func (s *transferState) isReady(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready[sessionID]
}

func (s *transferState) clearReady(sessionID string) {
	s.mu.Lock()
	delete(s.ready, sessionID)
	s.mu.Unlock()
}

func (s *Server) handleTransferBegin(writer *connectionWriter, msg protocol.Message) {
	begin, err := protocol.DecodePayload[protocol.TransferBegin](msg)
	if err != nil {
		writer.sendError("invalid_transfer", "invalid transfer begin", msg.SessionID)
		return
	}
	if err := s.transfers.begin(msg.SessionID, begin); err != nil {
		writer.sendError("transfer_failed", "transfer could not begin", msg.SessionID)
	}
}

func (s *Server) handleTransferChunk(writer *connectionWriter, msg protocol.Message) {
	chunk, err := protocol.DecodePayload[protocol.TransferChunk](msg)
	if err != nil {
		writer.sendError("invalid_transfer", "invalid transfer chunk", msg.SessionID)
		return
	}
	if err := s.transfers.chunk(msg.SessionID, chunk); err != nil {
		writer.sendError("transfer_failed", err.Error(), msg.SessionID)
	}
}

func (s *Server) handleTransferEnd(writer *connectionWriter, msg protocol.Message) {
	end, err := protocol.DecodePayload[protocol.TransferEnd](msg)
	if err != nil {
		writer.sendError("invalid_transfer", "invalid transfer end", msg.SessionID)
		return
	}
	payload, direction, err := s.transfers.end(msg.SessionID, end)
	if err != nil {
		writer.sendError("transfer_failed", err.Error(), msg.SessionID)
		return
	}
	if direction != "to_runner" {
		return
	}
	repositoryPath := s.manager.SessionWorkspacePath(msg.SessionID)
	if err := workspace.MaterializeBundle(context.Background(), repositoryPath, payload); err != nil {
		writer.sendError("transfer_failed", "workspace could not be materialized", msg.SessionID)
		return
	}
	s.transfers.markReady(msg.SessionID)
}

func (w *connectionWriter) sendTransfer(ctx context.Context, sessionID, transferID, direction string, payload []byte) error {
	checksum := workspace.TransferChecksum(payload)
	if err := w.send(protocol.TypeTransferBegin, sessionID, protocol.TransferBegin{
		TransferID: transferID,
		Direction:  direction,
		TotalBytes: int64(len(payload)),
		Checksum:   checksum,
	}); err != nil {
		return err
	}
	chunkSize := 64 << 10
	for offset := 0; offset < len(payload); offset += chunkSize {
		end := offset + chunkSize
		if end > len(payload) {
			end = len(payload)
		}
		if err := w.send(protocol.TypeTransferChunk, sessionID, protocol.TransferChunk{
			TransferID: transferID,
			Data:       base64.StdEncoding.EncodeToString(payload[offset:end]),
		}); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return w.send(protocol.TypeTransferEnd, sessionID, protocol.TransferEnd{TransferID: transferID})
}

func (s *Server) syncWorkspaceBack(writer streamWriter, sessionID string) {
	s.transfers.clearReady(sessionID)
	repositoryPath := s.manager.SessionWorkspacePath(sessionID)
	if !workspace.IsRepository(context.Background(), repositoryPath) {
		_ = os.RemoveAll(repositoryPath)
		return
	}
	payload, err := workspace.SnapshotBundle(context.Background(), repositoryPath, sessionID+"-sync")
	if err != nil {
		writer.sendError("transfer_failed", "workspace sync snapshot failed", sessionID)
		return
	}
	if len(payload) == 0 {
		_ = os.RemoveAll(repositoryPath)
		return
	}
	if err := writer.sendTransfer(context.Background(), sessionID, sessionID+"-sync", "from_runner", payload); err != nil {
		writer.sendError("transfer_failed", "workspace sync transfer failed", sessionID)
		return
	}
	_ = os.RemoveAll(repositoryPath)
}
