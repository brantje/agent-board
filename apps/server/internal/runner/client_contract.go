package runner

import (
	"context"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

// Client is the provider-neutral execution transport exposed by a connected
// agent-runner. Durable ownership and placement remain outside this transport.
type Client interface {
	Capabilities() protocol.Capabilities
	Health() protocol.Health
	Start(context.Context, string, Request) (ProcessSession, error)
	Attach(string) (ProcessSession, error)
	Done() <-chan struct{}
	Err() error
	Close() error
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
	if client == nil {
		return false
	}
	select {
	case <-client.Done():
		return false
	default:
		return true
	}
}
