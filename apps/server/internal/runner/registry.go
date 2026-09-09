package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

type Authenticator interface {
	Authenticate(context.Context, string, string) (store.Runner, error)
}
type Observer interface {
	ObserveRunner(context.Context, string, json.RawMessage) error
}

// Registry holds authenticated live transports only. Durable reservations and
// execution ownership remain in PostgreSQL and the existing scheduler.
type Registry struct {
	auth     Authenticator
	observer Observer
	mu       sync.RWMutex
	entries  map[string]*Connection
	closed   bool
}

func NewRegistry(auth Authenticator, observer Observer) *Registry {
	return &Registry{auth: auth, observer: observer, entries: map[string]*Connection{}}
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	header := req.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || req.URL.RawQuery != "" || req.Header.Get("Origin") != "" {
		http.Error(w, "runner authentication required", 401)
		return
	}
	token := strings.TrimPrefix(header, "Bearer ")
	id := req.Header.Get(protocol.RunnerIDHeader)
	identity, err := r.auth.Authenticate(req.Context(), id, token)
	if err != nil {
		http.Error(w, "runner authentication required", 401)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(req *http.Request) bool { return req.Header.Get("Origin") == "" }}
	socket, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	client, err := acceptConnection(req.Context(), socket)
	if err != nil {
		return
	}
	if err = client.Capabilities().Validate(); err != nil {
		_ = client.write(protocol.TypeError, "", protocol.ErrorPayload{Code: "invalid_capabilities", Message: "runner capabilities are invalid"})
		_ = client.Close()
		return
	}
	// Revalidate after negotiation so rotation/revocation during upgrade cannot
	// leave a newly eligible transport authenticated with an invalid credential.
	if _, err = r.auth.Authenticate(req.Context(), identity.ID, token); err != nil {
		_ = client.Close()
		return
	}
	encoded, _ := json.Marshal(client.Capabilities())
	if r.observer != nil {
		if err = r.observer.ObserveRunner(req.Context(), identity.ID, encoded); err != nil {
			_ = client.Close()
			return
		}
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = client.Close()
		return
	}
	old := r.entries[identity.ID]
	r.entries[identity.ID] = client
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	go func() {
		<-client.Done()
		r.mu.Lock()
		if r.entries[identity.ID] == client {
			delete(r.entries, identity.ID)
		}
		r.mu.Unlock()
	}()
}
func (r *Registry) Connected(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := r.entries[id]
	return !r.closed && c != nil && clientAlive(c)
}

func (r *Registry) Candidates(engine string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := []string{}
	if r.closed {
		return ids
	}
	for id, c := range r.entries {
		if !clientAlive(c) || c.Health().ActiveSessions > 0 {
			continue
		}
		for _, supported := range c.Capabilities().Engines {
			if supported == engine {
				ids = append(ids, id)
				break
			}
		}
	}
	return ids
}
func (r *Registry) Connect(_ context.Context, _ string, id string) (Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := r.entries[id]
	if r.closed || c == nil || !clientAlive(c) {
		return nil, ErrDisconnected
	}
	return c, nil
}
func (r *Registry) Reconcile(ctx context.Context, projectID, id, sessionID string) (ProcessSession, bool, error) {
	c, err := r.Connect(ctx, projectID, id)
	if err != nil {
		return nil, false, err
	}
	active := containsSession(c.Health().ActiveSessionIDs, sessionID)
	process, err := c.Attach(sessionID)
	return process, active, err
}
func (r *Registry) Disconnect(id string) {
	r.mu.Lock()
	c := r.entries[id]
	delete(r.entries, id)
	r.mu.Unlock()
	if c != nil {
		for _, id := range c.Health().ActiveSessionIDs {
			_ = c.writeSessionSignal(id, true)
		}
		_ = c.Close()
	}
}
func (r *Registry) Close() error {
	r.mu.Lock()
	r.closed = true
	clients := r.entries
	r.entries = map[string]*Connection{}
	r.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
	return nil
}
