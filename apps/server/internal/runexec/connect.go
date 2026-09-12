package runexec

import (
	"context"
	"fmt"
	"net"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type sessionDialService interface {
	DialSession(context.Context, string, string, string, string) (net.Conn, error)
}

// DialContext lets an Engine connect only through the Runner-owned Execution
// Session that owns this process. The adapter sees only the standard net.Conn
// contract and never receives Runner or WebSocket objects.
func (p *capturingProcess) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if p == nil || p.process == nil || p.launcher == nil {
		return nil, fmt.Errorf("run execution: process session connector is unavailable")
	}
	dialer, ok := p.launcher.sessions.(sessionDialService)
	if !ok {
		return nil, fmt.Errorf("run execution: process session connector is unsupported")
	}
	record := p.process.Record()
	if record.ID == "" || record.ProjectID == "" || record.RunnerID == "" {
		return nil, fmt.Errorf("run execution: process has no durable Runner-owned Execution Session binding")
	}
	return dialer.DialSession(ctx, record.ProjectID, record.ID, network, address)
}

var _ engine.SessionConnector = (*capturingProcess)(nil)
