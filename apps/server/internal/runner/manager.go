package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

var ErrManagerClosed = errors.New("runner: connection manager closed")

type EndpointResolver interface {
	RunnerEndpoint(context.Context, string, string) (runtimepkg.RunnerEndpoint, error)
}

type StatusReporter interface {
	SetRunnerStatus(context.Context, string, string, string) error
}

type Client interface {
	Capabilities() protocol.Capabilities
	Health() protocol.Health
	Start(context.Context, string, Request) (ProcessSession, error)
	Attach(string) (ProcessSession, error)
	Done() <-chan struct{}
	Err() error
	Close() error
}

type DialFunc func(context.Context, string) (Client, error)

type managerEntry struct {
	client      Client
	connecting chan struct{}
	lastErr     error
}

type Manager struct {
	resolver EndpointResolver
	reporter StatusReporter
	dial     DialFunc

	mu      sync.Mutex
	entries map[string]*managerEntry
	closed  bool
}

func NewManager(resolver EndpointResolver, reporter StatusReporter) (*Manager, error) {
	return newManager(resolver, reporter, func(ctx context.Context, endpoint string) (Client, error) {
		return Dial(ctx, endpoint)
	})
}

func newManager(resolver EndpointResolver, reporter StatusReporter, dial DialFunc) (*Manager, error) {
	if resolver == nil || dial == nil {
		return nil, fmt.Errorf("runner manager resolver and dialer are required")
	}
	return &Manager{resolver: resolver, reporter: reporter, dial: dial, entries: make(map[string]*managerEntry)}, nil
}

func (m *Manager) Connect(ctx context.Context, projectID, runtimeInstanceID string) (Client, error) {
	if projectID == "" || runtimeInstanceID == "" {
		return nil, fmt.Errorf("runner manager project and Runtime Instance ids are required")
	}
	key := projectID + "/" + runtimeInstanceID
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrManagerClosed
		}
		entry := m.entries[key]
		if entry == nil {
			entry = &managerEntry{}
			m.entries[key] = entry
		}
		if entry.client != nil && clientAlive(entry.client) {
			client := entry.client
			m.mu.Unlock()
			return client, nil
		}
		if entry.connecting != nil {
			wait := entry.connecting
			m.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		wait := make(chan struct{})
		entry.connecting = wait
		m.mu.Unlock()

		m.report(ctx, projectID, runtimeInstanceID, "CONNECTING")
		endpoint, err := m.resolver.RunnerEndpoint(ctx, projectID, runtimeInstanceID)
		var client Client
		if err == nil {
			if endpoint.URL == "" {
				err = fmt.Errorf("runner endpoint URL is empty")
			} else {
				client, err = m.dial(ctx, endpoint.URL)
			}
		}

		m.mu.Lock()
		entry.connecting = nil
		entry.lastErr = err
		if err == nil && !m.closed {
			entry.client = client
		} else if client != nil {
			_ = client.Close()
		}
		close(wait)
		closed := m.closed
		m.mu.Unlock()

		if closed {
			return nil, ErrManagerClosed
		}
		if err != nil {
			m.report(ctx, projectID, runtimeInstanceID, "UNAVAILABLE")
			return nil, err
		}
		status := "READY"
		if client.Health().ActiveSessions > 0 {
			status = "BUSY"
		}
		m.report(ctx, projectID, runtimeInstanceID, status)
		go m.watch(key, projectID, runtimeInstanceID, client)
		return client, nil
	}
}

// Reconcile always attaches the expected durable session locally. The runner
// reattaches retained delivery immediately after handshake, so terminal output
// may arrive even when health no longer lists the session as active.
func (m *Manager) Reconcile(ctx context.Context, projectID, runtimeInstanceID, expectedSessionID string) (ProcessSession, bool, error) {
	client, err := m.Connect(ctx, projectID, runtimeInstanceID)
	if err != nil {
		return nil, false, err
	}
	if expectedSessionID == "" {
		return nil, false, nil
	}
	health := client.Health()
	active := containsSession(health.ActiveSessionIDs, expectedSessionID)
	session, err := client.Attach(expectedSessionID)
	if err != nil {
		return nil, active, err
	}
	status := "READY"
	if health.ActiveSessions > 0 {
		status = "BUSY"
	}
	m.report(ctx, projectID, runtimeInstanceID, status)
	return session, active, nil
}

func containsSession(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func clientAlive(client Client) bool {
	select {
	case <-client.Done():
		return false
	default:
		return true
	}
}

func (m *Manager) watch(key, projectID, runtimeInstanceID string, client Client) {
	<-client.Done()
	m.mu.Lock()
	entry := m.entries[key]
	if entry != nil && entry.client == client {
		entry.client = nil
		entry.lastErr = client.Err()
	}
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.report(ctx, projectID, runtimeInstanceID, "UNAVAILABLE")
}

func (m *Manager) report(ctx context.Context, projectID, runtimeInstanceID, status string) {
	if m.reporter != nil {
		_ = m.reporter.SetRunnerStatus(ctx, projectID, runtimeInstanceID, status)
	}
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	clients := make([]Client, 0, len(m.entries))
	for _, entry := range m.entries {
		if entry.client != nil {
			clients = append(clients, entry.client)
		}
	}
	m.mu.Unlock()
	var closeErrors []error
	for _, client := range clients {
		if err := client.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}
