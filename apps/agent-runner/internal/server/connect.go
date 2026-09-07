package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

const (
	connectDialTimeout = 5 * time.Second
	connectChunkSize   = 32 * 1024
	connectQueueDepth  = 32
)

type sessionConnector struct {
	server    *Server
	sessionID string
	id        string
	owner     *connectionWriter
	conn      net.Conn
	writes    chan []byte
	done      chan struct{}
	once      sync.Once
}

func (s *Server) handleConnect(writer *connectionWriter, msg protocol.Message) {
	request, err := protocol.DecodePayload[protocol.ConnectRequest](msg)
	if err != nil || strings.TrimSpace(request.ConnectionID) == "" {
		s.sendConnectClose(writer, msg.SessionID, request.ConnectionID, "invalid_connect", "invalid connection request")
		return
	}
	if _, err := s.manager.Get(msg.SessionID); err != nil {
		s.sendConnectClose(writer, msg.SessionID, request.ConnectionID, "session_not_found", "execution session is not active")
		return
	}
	if err := validateConnectDestination(request.Network, request.Address); err != nil {
		s.sendConnectClose(writer, msg.SessionID, request.ConnectionID, "address_not_allowed", "connection destination is not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(s.shutdownCtx, connectDialTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, request.Network, request.Address)
	if err != nil {
		s.sendConnectClose(writer, msg.SessionID, request.ConnectionID, "connect_failed", "connection could not be established")
		return
	}
	connector := &sessionConnector{
		server:    s,
		sessionID: msg.SessionID,
		id:        request.ConnectionID,
		owner:     writer,
		conn:      conn,
		writes:    make(chan []byte, connectQueueDepth),
		done:      make(chan struct{}),
	}
	if !s.registerConnector(connector) {
		_ = conn.Close()
		s.sendConnectClose(writer, msg.SessionID, request.ConnectionID, "duplicate_connection", "connection id already exists")
		return
	}
	if err := writer.send(protocol.TypeConnected, msg.SessionID, protocol.Connected{ConnectionID: request.ConnectionID}); err != nil {
		connector.close(false, "", "")
		return
	}

	s.streamWG.Add(2)
	go connector.readLoop()
	go connector.writeLoop()
	go s.closeConnectorWithSession(connector)
}

func (s *Server) handleConnectData(writer *connectionWriter, msg protocol.Message) {
	payload, err := protocol.DecodePayload[protocol.ConnectData](msg)
	if err != nil || strings.TrimSpace(payload.ConnectionID) == "" {
		s.sendConnectClose(writer, msg.SessionID, payload.ConnectionID, "invalid_connect_data", "invalid connection data")
		return
	}
	connector := s.connector(msg.SessionID, payload.ConnectionID)
	if connector == nil || connector.owner != writer {
		s.sendConnectClose(writer, msg.SessionID, payload.ConnectionID, "connection_not_found", "session connection is not active")
		return
	}
	if len(payload.Data) == 0 {
		return
	}
	chunk := append([]byte(nil), payload.Data...)
	select {
	case connector.writes <- chunk:
	case <-connector.done:
		s.sendConnectClose(writer, msg.SessionID, payload.ConnectionID, "connection_closed", "session connection is closed")
	default:
		connector.close(true, "connect_backpressure", "connection write queue is full")
	}
}

func (s *Server) handleConnectClose(writer *connectionWriter, msg protocol.Message) {
	payload, err := protocol.DecodePayload[protocol.ConnectClose](msg)
	if err != nil || strings.TrimSpace(payload.ConnectionID) == "" {
		s.sendConnectClose(writer, msg.SessionID, payload.ConnectionID, "invalid_connect_close", "invalid connection close")
		return
	}
	connector := s.connector(msg.SessionID, payload.ConnectionID)
	if connector == nil || connector.owner != writer {
		return
	}
	connector.close(true, "", "")
}

func (s *Server) registerConnector(connector *sessionConnector) bool {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	byID := s.connectors[connector.sessionID]
	if byID == nil {
		byID = make(map[string]*sessionConnector)
		s.connectors[connector.sessionID] = byID
	}
	if _, exists := byID[connector.id]; exists {
		return false
	}
	byID[connector.id] = connector
	return true
}

func (s *Server) connector(sessionID, connectionID string) *sessionConnector {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	return s.connectors[sessionID][connectionID]
}

func (s *Server) removeConnector(connector *sessionConnector) {
	s.connectMu.Lock()
	byID := s.connectors[connector.sessionID]
	if byID != nil && byID[connector.id] == connector {
		delete(byID, connector.id)
		if len(byID) == 0 {
			delete(s.connectors, connector.sessionID)
		}
	}
	s.connectMu.Unlock()
}

func (s *Server) detachConnectors(writer *connectionWriter) {
	s.connectMu.Lock()
	var detached []*sessionConnector
	for _, byID := range s.connectors {
		for _, connector := range byID {
			if connector.owner == writer {
				detached = append(detached, connector)
			}
		}
	}
	s.connectMu.Unlock()
	for _, connector := range detached {
		connector.close(false, "", "")
	}
}

func (s *Server) closeConnectorWithSession(connector *sessionConnector) {
	execution, err := s.manager.Get(connector.sessionID)
	if err != nil {
		connector.close(true, "session_ended", "execution session ended")
		return
	}
	select {
	case <-execution.Done():
		connector.close(true, "session_ended", "execution session ended")
	case <-connector.done:
	case <-s.shutdownCtx.Done():
		connector.close(false, "", "")
	}
}

func (s *Server) sendConnectClose(writer *connectionWriter, sessionID, connectionID, code, message string) {
	_ = writer.send(protocol.TypeConnectClose, sessionID, protocol.ConnectClose{
		ConnectionID: connectionID,
		Code:         code,
		Message:      message,
	})
}

func (c *sessionConnector) readLoop() {
	defer c.server.streamWG.Done()
	buffer := make([]byte, connectChunkSize)
	for {
		n, err := c.conn.Read(buffer)
		if n > 0 {
			if sendErr := c.owner.send(protocol.TypeConnectData, c.sessionID, protocol.ConnectData{
				ConnectionID: c.id,
				Data:         append([]byte(nil), buffer[:n]...),
			}); sendErr != nil {
				c.close(false, "", "")
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.close(true, "", "")
			} else {
				c.close(true, "connection_read_failed", "session connection read failed")
			}
			return
		}
	}
}

func (c *sessionConnector) writeLoop() {
	defer c.server.streamWG.Done()
	for {
		select {
		case data := <-c.writes:
			if len(data) == 0 {
				continue
			}
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				c.close(true, "connection_write_failed", "session connection write failed")
				return
			}
			if _, err := c.conn.Write(data); err != nil {
				c.close(true, "connection_write_failed", "session connection write failed")
				return
			}
		case <-c.done:
			return
		case <-c.server.shutdownCtx.Done():
			c.close(false, "", "")
			return
		}
	}
}

func (c *sessionConnector) close(notify bool, code, message string) {
	c.once.Do(func() {
		c.server.removeConnector(c)
		close(c.done)
		_ = c.conn.Close()
		if notify {
			c.server.sendConnectClose(c.owner, c.sessionID, c.id, code, message)
		}
	})
}

func validateConnectDestination(network, address string) error {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return fmt.Errorf("unsupported network %q", network)
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid port")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("destination is not loopback")
	}
	return nil
}
