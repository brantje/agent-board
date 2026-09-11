package server

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

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
	mu              sync.Mutex
	incoming        map[string]*incomingTransfer
	ready           map[string]workspace.CheckoutState
	begun           map[string]bool
	failed          map[string]bool
	awaitingApplied map[string]string
}

func newTransferState() *transferState {
	return &transferState{
		incoming:        make(map[string]*incomingTransfer),
		ready:           make(map[string]workspace.CheckoutState),
		begun:           make(map[string]bool),
		failed:          make(map[string]bool),
		awaitingApplied: make(map[string]string),
	}
}

func (s *transferState) begin(sessionID string, begin protocol.TransferBegin) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("transfer requires session and transfer ids")
	}
	if err := protocol.ValidateTransferBegin(begin); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.begun[sessionID] = true
	delete(s.failed, sessionID)
	if begin.Direction == "to_runner" {
		delete(s.ready, sessionID)
	}
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
	buffer, err := protocol.AppendTransferChunk(transfer.buffer, transfer.expected, chunk)
	if err != nil {
		return err
	}
	transfer.buffer = buffer
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

	if err := protocol.ValidateTransferPayload(payload, transfer.expected, transfer.checksum); err != nil {
		return nil, "", err
	}
	return payload, direction, nil
}

func (s *transferState) markReady(sessionID string, state workspace.CheckoutState) {
	s.mu.Lock()
	s.ready[sessionID] = state
	delete(s.failed, sessionID)
	s.mu.Unlock()
}

func (s *transferState) checkout(sessionID string) (workspace.CheckoutState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.ready[sessionID]
	return state, ok
}

func (s *transferState) markFailed(sessionID string) {
	s.mu.Lock()
	s.failed[sessionID] = true
	delete(s.ready, sessionID)
	s.mu.Unlock()
}

func (s *transferState) waitReady(sessionID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		_, ready := s.ready[sessionID]
		failed := s.failed[sessionID]
		begun := s.begun[sessionID]
		incoming := s.incoming[sessionID] != nil
		s.mu.Unlock()
		if ready {
			return true
		}
		if failed {
			return false
		}
		if !begun && !incoming {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *transferState) clearReady(sessionID string) {
	s.mu.Lock()
	delete(s.ready, sessionID)
	delete(s.begun, sessionID)
	delete(s.failed, sessionID)
	s.mu.Unlock()
}

func (s *transferState) awaitApplied(sessionID, transferID string) {
	s.mu.Lock()
	s.awaitingApplied[sessionID] = transferID
	s.mu.Unlock()
}

func (s *transferState) matchesApplied(sessionID, transferID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.awaitingApplied[sessionID] == transferID && transferID != ""
}

func (s *transferState) clearApplied(sessionID, transferID string) {
	s.mu.Lock()
	if s.awaitingApplied[sessionID] == transferID {
		delete(s.awaitingApplied, sessionID)
	}
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
		s.transfers.markFailed(msg.SessionID)
		writer.sendError("transfer_failed", err.Error(), msg.SessionID)
		return
	}
	if direction == "from_runner" {
		s.syncWorkspaceBack(writer, msg.SessionID, end.TransferID)
		return
	}
	if direction != "to_runner" {
		return
	}
	repositoryPath := s.manager.SessionWorkspacePath(msg.SessionID)
	if repositoryPath == "" {
		s.transfers.markFailed(msg.SessionID)
		writer.sendError("transfer_failed", "workspace could not be materialized", msg.SessionID)
		return
	}
	state, err := workspace.MaterializeBranchBundle(context.Background(), repositoryPath, payload)
	if err != nil {
		s.transfers.markFailed(msg.SessionID)
		writer.sendError("transfer_failed", "workspace could not be materialized: "+err.Error(), msg.SessionID)
		return
	}
	s.transfers.markReady(msg.SessionID, state)
}

func (s *Server) handleTransferApplied(writer *connectionWriter, msg protocol.Message) {
	applied, err := protocol.DecodePayload[protocol.TransferApplied](msg)
	if err != nil || !s.transfers.matchesApplied(msg.SessionID, applied.TransferID) {
		writer.sendError("invalid_transfer_ack", "workspace transfer acknowledgement is invalid", msg.SessionID)
		return
	}
	repositoryPath := s.manager.SessionWorkspacePath(msg.SessionID)
	if repositoryPath == "" {
		writer.sendError("workspace_cleanup_failed", "session workspace path is unavailable", msg.SessionID)
		return
	}
	if err := os.RemoveAll(repositoryPath); err != nil {
		writer.sendError("workspace_cleanup_failed", "session workspace could not be removed", msg.SessionID)
		return
	}
	s.transfers.clearApplied(msg.SessionID, applied.TransferID)
	s.transfers.clearReady(msg.SessionID)
}

func (w *connectionWriter) sendTransfer(ctx context.Context, sessionID, transferID, direction string, payload []byte) error {
	if err := w.send(protocol.TypeTransferBegin, sessionID, protocol.TransferBegin{
		TransferID: transferID,
		Direction:  direction,
		TotalBytes: int64(len(payload)),
		Checksum:   protocol.TransferChecksum(payload),
	}); err != nil {
		return err
	}
	chunkSize := protocol.TransferChunkSize
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

func (s *Server) syncWorkspaceBack(writer streamWriter, sessionID, transferID string) {
	if strings.TrimSpace(transferID) == "" {
		transferID = sessionID + "-sync"
	}
	fail := func(message string) {
		_ = writer.send(protocol.TypeTransferFailed, sessionID, protocol.TransferFailed{
			TransferID: transferID,
			Code:       "transfer_failed",
			Message:    message,
		})
	}
	repositoryPath := s.manager.SessionWorkspacePath(sessionID)
	if repositoryPath == "" {
		fail("session workspace path is unavailable")
		return
	}
	state, ok := s.transfers.checkout(sessionID)
	if !ok {
		fail("workspace execution-start state is unavailable")
		return
	}
	if !workspace.IsRepository(context.Background(), repositoryPath) {
		fail("session workspace is not a Git repository")
		return
	}
	if _, err := workspace.FinalizeCheckout(context.Background(), repositoryPath, state); err != nil {
		fail("workspace finalization failed: " + err.Error())
		return
	}
	payload, err := workspace.SnapshotBundle(context.Background(), repositoryPath, transferID)
	if err != nil || len(payload) == 0 {
		if err != nil {
			fail("workspace branch snapshot failed: " + err.Error())
		} else {
			fail("workspace branch snapshot is empty")
		}
		return
	}
	s.transfers.awaitApplied(sessionID, transferID)
	if err := writer.sendTransfer(context.Background(), sessionID, transferID, "from_runner", payload); err != nil {
		fail("workspace branch transfer failed: " + err.Error())
		return
	}
}
