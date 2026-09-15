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

// WorkingDirectoryProvider is an optional Process capability that reports the
// Runner-resolved Execution Session working directory. OpenCode session APIs
// must use this host path; logical /workspace is a different OpenCode project
// on persistent runners, and omitting it falls back to ~/projects/issue.
type WorkingDirectoryProvider interface {
	WorkingDirectory() string
}
