package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPreparePublishedWorkspaceGrantsRuntimeAccessWithoutRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are required")
	}
	if os.Getuid() == 0 {
		t.Skip("non-root chmod path is required")
	}

	root := t.TempDir()
	workspacePath := filepath.Join(root, "issue-workspace")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	if err := PreparePublishedWorkspace(workspacePath, RuntimeIdentity{UID: 100, GID: 101}); err != nil {
		t.Fatalf("PreparePublishedWorkspace() error = %v", err)
	}

	info, err := os.Stat(workspacePath)
	if err != nil {
		t.Fatalf("Stat(workspace) error = %v", err)
	}
	if info.Mode().Perm()&0o005 == 0 {
		t.Fatalf("workspace mode = %#o, want group/other traverse permission", info.Mode().Perm())
	}
}

func TestPreparePublishedWorkspaceGrantsRuntimeAccessAsRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership permissions are required")
	}
	if os.Getuid() != 0 {
		t.Skip("root chown path is required")
	}

	root := t.TempDir()
	workspacePath := filepath.Join(root, "issue-workspace")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(workspacePath, "src"), 0o700); err != nil {
		t.Fatalf("Mkdir(src) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "src", "main.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	uid := os.Getuid()
	gid := os.Getgid()
	if err := PreparePublishedWorkspace(workspacePath, RuntimeIdentity{UID: uid, GID: gid}); err != nil {
		t.Fatalf("PreparePublishedWorkspace() error = %v", err)
	}

	info, err := os.Stat(workspacePath)
	if err != nil {
		t.Fatalf("Stat(workspace) error = %v", err)
	}
	if info.Mode().Perm()&0o005 == 0 {
		t.Fatalf("workspace mode = %#o, want group/other traverse permission", info.Mode().Perm())
	}

	srcInfo, err := os.Stat(filepath.Join(workspacePath, "src"))
	if err != nil {
		t.Fatalf("Stat(src) error = %v", err)
	}
	if srcInfo.Mode().Perm()&0o005 == 0 {
		t.Fatalf("src mode = %#o, want group/other traverse permission", srcInfo.Mode().Perm())
	}
}

func TestRuntimeIdentityFromEnvDefaultsToAgentRunnerImageUser(t *testing.T) {
	t.Setenv("AGENT_BOARD_RUNTIME_UID", "")
	t.Setenv("AGENT_BOARD_RUNTIME_GID", "")

	identity := RuntimeIdentityFromEnv()
	if identity.UID != 100 || identity.GID != 101 {
		t.Fatalf("RuntimeIdentityFromEnv() = %+v, want UID=100 GID=101", identity)
	}
}

func TestRuntimeIdentityFromEnvUsesConfiguredValues(t *testing.T) {
	t.Setenv("AGENT_BOARD_RUNTIME_UID", "1000")
	t.Setenv("AGENT_BOARD_RUNTIME_GID", "1000")

	identity := RuntimeIdentityFromEnv()
	if identity.UID != 1000 || identity.GID != 1000 {
		t.Fatalf("RuntimeIdentityFromEnv() = %+v, want UID=1000 GID=1000", identity)
	}
}
