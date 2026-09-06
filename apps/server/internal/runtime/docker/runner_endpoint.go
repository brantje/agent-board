package docker

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
)

const (
	runnerPort = "8080"
	runnerPath = "/v1/ws"
)

// RunnerEndpoint resolves the runner address from Docker's inspected network
// state. No Docker handle or client escapes the Runtime implementation.
func (r *Runtime) RunnerEndpoint(ctx context.Context, handle runtimepkg.Handle) (runtimepkg.RunnerEndpoint, error) {
	inspected, meta, err := r.inspectOwned(ctx, handle)
	if err != nil {
		return runtimepkg.RunnerEndpoint{}, err
	}
	if inspected.State == nil || !inspected.State.Running {
		return runtimepkg.RunnerEndpoint{}, fmt.Errorf("%w: Runtime Instance is not running", runtimepkg.ErrRunnerUnavailable)
	}
	if meta.Network == runtimepkg.NetworkNone {
		return runtimepkg.RunnerEndpoint{}, fmt.Errorf("%w: Docker network policy none has no TCP control path", runtimepkg.ErrRunnerUnavailable)
	}
	if inspected.NetworkSettings == nil {
		return runtimepkg.RunnerEndpoint{}, fmt.Errorf("%w: Docker network settings are unavailable", runtimepkg.ErrRunnerUnavailable)
	}

	endpoint := inspected.NetworkSettings.Networks["bridge"]
	if endpoint == nil || !endpoint.IPAddress.IsValid() {
		return runtimepkg.RunnerEndpoint{}, fmt.Errorf("%w: Docker bridge network has no routable address", runtimepkg.ErrRunnerUnavailable)
	}
	host := strings.TrimSpace(endpoint.IPAddress.String())
	if host == "" {
		return runtimepkg.RunnerEndpoint{}, fmt.Errorf("%w: Docker bridge network has no routable address", runtimepkg.ErrRunnerUnavailable)
	}

	u := url.URL{Scheme: "ws", Host: net.JoinHostPort(host, runnerPort), Path: runnerPath}
	return runtimepkg.RunnerEndpoint{URL: u.String()}, nil
}
