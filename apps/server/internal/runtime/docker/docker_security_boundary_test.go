package docker

import (
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
)

func TestVerifyContainerRequiresNoNewPrivileges(t *testing.T) {
	spec := dockerSpec()

	for _, securityOpt := range [][]string{
		{"no-new-privileges"},
		{"no-new-privileges=true"},
		{"no-new-privileges:true"},
	} {
		t.Run(securityOpt[0], func(t *testing.T) {
			inspected := ownedInspect(spec, "container-1", "running")
			inspected.Container.HostConfig.SecurityOpt = securityOpt
			if err := verifyContainer(inspected.Container, metadataFromSpec(spec)); err != nil {
				t.Fatalf("verifyContainer() error=%v", err)
			}
		})
	}

	for name, securityOpt := range map[string][]string{
		"missing": nil,
		"disabled": {"no-new-privileges=false"},
	} {
		t.Run(name, func(t *testing.T) {
			inspected := ownedInspect(spec, "container-1", "running")
			inspected.Container.HostConfig.SecurityOpt = securityOpt
			if err := verifyContainer(inspected.Container, metadataFromSpec(spec)); !errors.Is(err, runtimepkg.ErrOwnershipMismatch) {
				t.Fatalf("verifyContainer() error=%v", err)
			}
		})
	}
}
