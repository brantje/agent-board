package opencode

import (
	"fmt"
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
// agent-runner is Linux-only for v0.1, where the entire 127/8 range is
// loopback. Combining a run-scoped loopback address and high port keeps
// concurrent OpenCode servers on the same host isolated while preserving the
// exact endpoint across Runner reconnect/recovery.
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
	sum := hasher.Sum64()
	octet := func(shift uint) uint64 { return ((sum >> shift) % 254) + 1 }
	host := fmt.Sprintf("127.%d.%d.%d", octet(40), octet(24), octet(8))
	port := nativeServerPortBase + int(sum%nativeServerPortSpan)
	return net.JoinHostPort(host, strconv.Itoa(port))
}
