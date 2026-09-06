package runner

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
)

type fakeEndpointResolver struct {
	calls atomic.Int32
}

func (r *fakeEndpointResolver) RunnerEndpoint(context.Context, string, string) (runtimepkg.RunnerEndpoint, error) {
	r.calls.Add(1)
	return runtimepkg.RunnerEndpoint{URL: "ws://runner.test/v1/ws"}, nil
}

type fakeManagerClient struct {
	health      protocol.Health
	done        chan struct{}
	closeOnce   sync.Once
	attachCalls atomic.Int32
}

func newFakeManagerClient() *fakeManagerClient {
	return &fakeManagerClient{done: make(chan struct{})}
}
func (c *fakeManagerClient) Capabilities() protocol.Capabilities { return protocol.Capabilities{MaxActiveSessions: 1} }
func (c *fakeManagerClient) Health() protocol.Health { return c.health }
func (c *fakeManagerClient) Start(context.Context, string, Request) (*Session, error) { return nil, nil }
func (c *fakeManagerClient) Attach(id string) (*Session, error) {
	c.attachCalls.Add(1)
	s := newSession(id, nil)
	s.markStarted(nil)
	return s, nil
}
func (c *fakeManagerClient) Done() <-chan struct{} { return c.done }
func (c *fakeManagerClient) Err() error { return nil }
func (c *fakeManagerClient) Close() error {
	c.closeOnce.Do(func() { close(c.done) })
	return nil
}

func TestManagerCollapsesConcurrentConnectionsAndReusesClient(t *testing.T) {
	resolver := &fakeEndpointResolver{}
	client := newFakeManagerClient()
	var dialCalls atomic.Int32
	manager, err := newManager(resolver, nil, func(context.Context, string) (Client, error) {
		dialCalls.Add(1)
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := manager.Connect(context.Background(), "project-1", "runtime-1")
			if err != nil || got != client {
				t.Errorf("Connect() client=%v err=%v", got, err)
			}
		}()
	}
	wg.Wait()
	if dialCalls.Load() != 1 || resolver.calls.Load() != 1 {
		t.Fatalf("dialCalls=%d resolverCalls=%d", dialCalls.Load(), resolver.calls.Load())
	}
}

func TestManagerReconnectsAfterDisconnect(t *testing.T) {
	resolver := &fakeEndpointResolver{}
	first := newFakeManagerClient()
	second := newFakeManagerClient()
	clients := []Client{first, second}
	var index atomic.Int32
	manager, _ := newManager(resolver, nil, func(context.Context, string) (Client, error) {
		i := index.Add(1) - 1
		return clients[i], nil
	})
	defer manager.Close()
	if _, err := manager.Connect(context.Background(), "project-1", "runtime-1"); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	got, err := manager.Connect(context.Background(), "project-1", "runtime-1")
	if err != nil || got != second {
		t.Fatalf("Connect() client=%v err=%v", got, err)
	}
}

func TestManagerReconcileAttachesWithoutStartingDuplicate(t *testing.T) {
	resolver := &fakeEndpointResolver{}
	client := newFakeManagerClient()
	client.health = protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"session-live"}}
	manager, _ := newManager(resolver, nil, func(context.Context, string) (Client, error) { return client, nil })
	defer manager.Close()
	session, active, err := manager.Reconcile(context.Background(), "project-1", "runtime-1", "session-live")
	if err != nil || !active || session == nil || session.ID() != "session-live" {
		t.Fatalf("Reconcile() session=%v active=%v err=%v", session, active, err)
	}
	if client.attachCalls.Load() != 1 {
		t.Fatalf("attachCalls=%d", client.attachCalls.Load())
	}
	_, active, err = manager.Reconcile(context.Background(), "project-1", "runtime-1", "other")
	if err != nil || active {
		t.Fatalf("Reconcile(other) active=%v err=%v", active, err)
	}
}
