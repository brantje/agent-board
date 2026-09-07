package engine

import (
	"context"
	"net"
)

// SessionConnector is an optional capability implemented by an Engine process
// when the trusted server can open connections to services local to that
// process's Execution Session. Implementations must keep Runtime transport
// details private; Engine adapters only receive a standard net.Conn.
type SessionConnector interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}
