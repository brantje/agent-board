package docker

import (
	"context"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

func TestDockerCreateCleansUpAfterPostCreateValidationFailure(t *testing.T) {
	spec := dockerSpec()

	t.Run("inspect failure", func(t *testing.T) {
		inspectErr := errors.New("created inspect failed")
		fake := &errorMoby{fakeMoby: &fakeMoby{inspectFn: func(ref string) (client.ContainerInspectResult, error) {
			if ref == containerName(spec.RuntimeInstanceID) {
				return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
			}
			return client.ContainerInspectResult{}, inspectErr
		}}}
		runtime, _ := newWithClient(fake)

		_, err := runtime.Create(context.Background(), spec)
		if !errors.Is(err, inspectErr) || fake.removeCalls != 1 {
			t.Fatalf("Create() error=%v removeCalls=%d", err, fake.removeCalls)
		}
	})

	t.Run("verification failure preserves validation error when cleanup fails", func(t *testing.T) {
		removeErr := errors.New("remove failed")
		fake := &errorMoby{fakeMoby: &fakeMoby{inspectFn: func(ref string) (client.ContainerInspectResult, error) {
			if ref == containerName(spec.RuntimeInstanceID) {
				return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
			}
			inspected := ownedInspect(spec, "created-id", "created")
			inspected.Container.Config.Image = "unauthorized:image"
			return inspected, nil
		}}, removeErr: removeErr}
		runtime, _ := newWithClient(fake)

		_, err := runtime.Create(context.Background(), spec)
		if !errors.Is(err, runtimepkg.ErrOwnershipMismatch) || errors.Is(err, removeErr) || fake.removeCalls != 1 {
			t.Fatalf("Create() error=%v removeCalls=%d", err, fake.removeCalls)
		}
	})
}
