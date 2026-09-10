package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

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

// ConnectionReconciler authorizes replacing a live transport for an immutable
// runner identity. Durable session ownership remains in the application/store;
// the registry only supplies the old and replacement transport health claims.
type ConnectionReconciler interface {
	ReconcileRunnerConnection(context.Context, string, protocol.Health, protocol.Health) error
}

// Registry holds authenticated live transports only. Durable reservations and
// execution ownership remain in PostgreSQL and the existing scheduler.
type Registry struct {
	auth       Authenticator
	observer   Observer
	reconciler ConnectionReconciler

	installMu    sync.Mutex
	mu           sync.RWMutex
	entries      map[string]*Connection
	disconnected map[string]time.Time
	closed       bool
}

func NewRegistry(auth Authenticator, observer Observer) *Registry {
	return &Registry{auth: auth, observer: observer, entries: map[string]*Connection{}, disconnected: map[string]time.Time{}}
}

func (r *Registry) SetConnectionReconciler(reconciler ConnectionReconciler) {
	r.mu.Lock()
	r.reconciler = reconciler
	r.mu.Unlock()
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

	// Serialize installs for one registry so two simultaneous reconnects cannot
	// both reconcile against and replace the same transport.
	r.installMu.Lock()
	defer r.installMu.Unlock()

	r.mu.RLock()
	closed := r.closed
	old := r.entries[identity.ID]
	reconciler := r.reconciler
	r.mu.RUnlock()
	if closed {
		_ = client.Close()
		return
	}

	var oldHealth protocol.Health
	if old != nil && clientAlive(old) {
		oldHealth = old.Health()
	}
	if reconciler != nil {
		if err = reconciler.ReconcileRunnerConnection(req.Context(), identity.ID, oldHealth, client.Health()); err != nil {
			_ = client.write(protocol.TypeError, "", protocol.ErrorPayload{Code: "runner_session_conflict", Message: "runner connection cannot replace active session transport"})
			_ = client.Close()
			return
		}
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
	old = r.entries[identity.ID]
	r.entries[identity.ID] = client
	delete(r.disconnected, identity.ID)
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	go func() {
		<-client.Done()
		r.mu.Lock()
		if r.entries[identity.ID] == client {
			delete(r.entries, identity.ID)
			if _, exists := r.disconnected[identity.ID]; !exists {
				r.disconnected[identity.ID] = time.Now()
			}
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

// DisconnectedSince returns when the current live transport for a runner was
// lost. The timestamp is in-memory by design because connected state itself is
// ephemeral. If a dead transport is observed before its cleanup goroutine runs,
// this call records the first observed disconnect time rather than using the
// age of any Execution Session.
func (r *Registry) DisconnectedSince(id string) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		if c := r.entries[id]; c != nil && clientAlive(c) {
			return time.Time{}, false
		}
	}
	if disconnectedAt, ok := r.disconnected[id]; ok {
		return disconnectedAt, true
	}
	disconnectedAt := time.Now()
	r.disconnected[id] = disconnectedAt
	return disconnectedAt, true
}

func (r *Registry) Disconnect(id string) {
	r.mu.Lock()
	c := r.entries[id]
	delete(r.entries, id)
	if _, exists := r.disconnected[id]; !exists {
		r.disconnected[id] = time.Now()
	}
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
