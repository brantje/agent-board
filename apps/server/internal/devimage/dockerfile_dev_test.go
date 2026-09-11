package devimage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDevImageIncludesWorkspacegitPackage(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	serverRoot := filepath.Join(filepath.Dir(file), "..", "..")
	dockerfile, err := os.ReadFile(filepath.Join(serverRoot, "Dockerfile.dev"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dockerfile), "COPY packages/workspacegit/") {
		t.Fatal("apps/server/Dockerfile.dev must copy packages/workspacegit so go mod download can resolve the replace directive")
	}

	compose, err := os.ReadFile(filepath.Join(serverRoot, "..", "..", "docker-compose.dev.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "./packages/workspacegit:/src/packages/workspacegit") {
		t.Fatal("docker-compose.dev.yml must bind-mount packages/workspacegit for air hot reload")
	}
}
