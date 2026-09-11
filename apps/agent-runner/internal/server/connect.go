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

var (
	connectDialContext  = (&net.Dialer{}).DialContext
	connectLookupIPAddr = net.DefaultResolver.LookupIPAddr
)

type sessionConnector struct {
	server    *Server
	sessionID string
	id        string
	owner     *connectionWriter
	writes    chan []byte
	done      chan struct{}
	once      sync.Once

	stateMu    sync.Mutex
	conn       net.Conn
	dialCancel context.CancelFunc
}

func (s *Server) handleConnect(writer *connectionWriter, msg protocol.Message) {
	request, err := protocol.DecodePayload[protocol.ConnectRequest](msg)
	if err != nil || strings.TrimSpace(request.ConnectionID) == "" {
		s.sendConnectClose(writer, msg.SessionID, request.ConnectionID, "invalid_connect", "invalid connection request")
		return
	}
	execution, err := s.manager.Get(msg.SessionID)
	if err != nil {
		s.sendConnectClose(writer, msg.SessionID, request.ConnectionID, "session_not_found", "execution session is not active")
		return
	}

	ctx, cancel := context.WithTimeout(s.shutdownCtx, connectDialTimeout)
	connector := &sessionConnector{
		server:     s,
		sessionID:  msg.SessionID,
		id:         request.ConnectionID,
		owner:      writer,
		writes:     make(chan []byte, connectQueueDepth),
		done:       make(chan struct{}),
		dialCancel: cancel,
	}
	if !s.registerConnector(connector) {
		cancel()
		s.sendConnectClose(writer, msg.SessionID, request.ConnectionID, "duplicate_connection", "connection id already exists")
		return
	}

	s.streamWG.Add(1)
	go s.dialConnector(ctx, execution.Done(), connector, request.Network, request.Address)
}

func (s *Server) dialConnector(ctx context.Context, sessionDone <-chan struct{}, connector *sessionConnector, network, address string) {
	defer s.streamWG.Done()
	go func() {
		select {
		case <-sessionDone:
			connector.cancelDial()
		case <-ctx.Done():
		}
	}()

	dialAddress, err := resolveConnectDestination(ctx, network, address)
	if err != nil {
		connector.cancelDial()
		select {
		case <-connector.done:
			return
		case <-sessionDone:
			connector.close(true, "session_ended", "execution session ended")
		case <-s.shutdownCtx.Done():
			connector.close(false, "", "")
		default:
			connector.close(true, "address_not_allowed", "connection destination is not allowed")
		}
		return
	}

	conn, err := connectDialContext(ctx, network, dialAddress)
	connector.cancelDial()
	if err != nil {
		select {
		case <-connector.done:
			return
		case <-sessionDone:
			connector.close(true, "session_ended", "execution session ended")
		case <-s.shutdownCtx.Done():
			connector.close(false, "", "")
		default:
			connector.close(true, "connect_failed", "connection could not be established")
		}
		return
	}
	if !connector.attachConn(conn) {
		_ = conn.Close()
		return
	}
	select {
	case <-connector.done:
		return
	default:
	}
	if err := connector.owner.send(protocol.TypeConnected, connector.sessionID, protocol.Connected{ConnectionID: connector.id}); err != nil {
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
	case <-connector.done:
		s.sendConnectClose(writer, msg.SessionID, payload.ConnectionID, "connection_closed", "session connection is closed")
		return
	default:
	}
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

func (c *sessionConnector) attachConn(conn net.Conn) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	select {
	case <-c.done:
		return false
	default:
	}
	c.conn = conn
	return true
}

func (c *sessionConnector) activeConn() net.Conn {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.conn
}

func (c *sessionConnector) cancelDial() {
	c.stateMu.Lock()
	cancel := c.dialCancel
	c.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *sessionConnector) readLoop() {
	defer c.server.streamWG.Done()
	conn := c.activeConn()
	if conn == nil {
		c.close(true, "connection_read_failed", "session connection read failed")
		return
	}
	buffer := make([]byte, connectChunkSize)
	for {
		n, err := conn.Read(buffer)
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
	conn := c.activeConn()
	if conn == nil {
		c.close(true, "connection_write_failed", "session connection write failed")
		return
	}
	for {
		select {
		case data := <-c.writes:
			if len(data) == 0 {
				continue
			}
			if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				c.close(true, "connection_write_failed", "session connection write failed")
				return
			}
			if _, err := conn.Write(data); err != nil {
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
		c.stateMu.Lock()
		cancel := c.dialCancel
		conn := c.conn
		c.stateMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if conn != nil {
			_ = conn.Close()
		}
		if notify {
			c.server.sendConnectClose(c.owner, c.sessionID, c.id, code, message)
		}
	})
}

func validateConnectDestination(network, address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), connectDialTimeout)
	defer cancel()
	_, err := resolveConnectDestination(ctx, network, address)
	return err
}

func resolveConnectDestination(ctx context.Context, network, address string) (string, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return "", fmt.Errorf("unsupported network %q", network)
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid port")
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return "", fmt.Errorf("destination is not loopback")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() || !networkAllowsIP(network, ip) {
			return "", fmt.Errorf("destination is not loopback")
		}
		return net.JoinHostPort(ip.String(), strconv.Itoa(port)), nil
	}

	resolved, err := connectLookupIPAddr(ctx, host)
	if err != nil || len(resolved) == 0 {
		return "", fmt.Errorf("destination is not loopback")
	}
	var selected net.IP
	for _, candidate := range resolved {
		if !candidate.IP.IsLoopback() {
			return "", fmt.Errorf("destination is not loopback")
		}
		if selected == nil && networkAllowsIP(network, candidate.IP) {
			selected = candidate.IP
		}
	}
	if selected == nil {
		return "", fmt.Errorf("destination is not loopback")
	}
	return net.JoinHostPort(selected.String(), strconv.Itoa(port)), nil
}

func networkAllowsIP(network string, ip net.IP) bool {
	switch network {
	case "tcp4":
		return ip.To4() != nil
	case "tcp6":
		return ip.To4() == nil
	default:
		return true
	}
}
