package opencode

import (
	"hash/fnv"
	"net"
	"strconv"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

const (
	nativeServerPortBase = 20_000
	nativeServerPortSpan = 40_000
)

// nativeServerAddress gives each durable Run a stable loopback endpoint.
// Keeping the host on 127.0.0.1 avoids depending on support for arbitrary
// 127/8 bind addresses while a run-scoped high port isolates concurrent
// OpenCode servers on the same host and remains stable across reconnects.
func (e *Engine) nativeServerAddress(safe executioncontext.SafeContext) string {
	if address := strings.TrimSpace(e.address); address != "" {
		return address
	}
	runID := strings.TrimSpace(safe.Run.ID)
	if runID == "" {
		return defaultAddress
	}

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(runID))
	port := nativeServerPortBase + int(hasher.Sum64()%nativeServerPortSpan)
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
