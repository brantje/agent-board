package runner

import (
	"context"
	"net"
)

// SessionDialer is an optional runner Client capability for opening a
// connection from agent-runner to a service local to an active Execution
// Session. The underlying Connection implements it when the negotiated runner
// exposes the session_connect feature.
type SessionDialer interface {
	DialSession(context.Context, string, string, string) (net.Conn, error)
}

var _ SessionDialer = (*Connection)(nil)
