package docker

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func TestRunnerEndpointResolvesInspectedContainerAddress(t *testing.T) {
	spec := dockerSpec()
	spec.Network = runtimepkg.NetworkOutbound
	handle, err := handleFromSpec(spec, "container-id")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeMoby{}
	fake.inspectFn = func(string) (client.ContainerInspectResult, error) {
		result := ownedInspect(spec, "container-id", "running")
		result.Container.NetworkSettings = &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{
			"bridge": {IPAddress: netip.MustParseAddr("172.17.0.23")},
		}}
		return result, nil
	}
	runtime, _ := newWithClient(fake)
	endpoint, err := runtime.RunnerEndpoint(context.Background(), handle)
	if err != nil {
		t.Fatalf("RunnerEndpoint() error=%v", err)
	}
	if endpoint.URL != "ws://172.17.0.23:8080/v1/ws" {
		t.Fatalf("RunnerEndpoint() URL=%q", endpoint.URL)
	}
}

func TestRunnerEndpointDoesNotWidenNetworkNone(t *testing.T) {
	spec := dockerSpec()
	handle, _ := handleFromSpec(spec, "container-id")
	fake := &fakeMoby{}
	fake.inspectFn = func(string) (client.ContainerInspectResult, error) {
		return ownedInspect(spec, "container-id", "running"), nil
	}
	runtime, _ := newWithClient(fake)
	if _, err := runtime.RunnerEndpoint(context.Background(), handle); !errors.Is(err, runtimepkg.ErrRunnerUnavailable) {
		t.Fatalf("RunnerEndpoint() error=%v, want ErrRunnerUnavailable", err)
	}
}
