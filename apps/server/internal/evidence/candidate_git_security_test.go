package evidence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGitOutputDoesNotExecuteWorkspaceControlledHelpers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses executable shell scripts")
	}

	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	writeFile(t, workspace, "tracked.txt", "base\n")
	writeFile(t, workspace, ".gitattributes", "*.txt diff=agent-controlled\n")
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "-m", "base")

	helperDir := t.TempDir()
	diffMarker := filepath.Join(helperDir, "diff-executed")
	fsmonitorMarker := filepath.Join(helperDir, "fsmonitor-executed")
	diffHelper := filepath.Join(helperDir, "diff-helper.sh")
	fsmonitorHelper := filepath.Join(helperDir, "fsmonitor-helper.sh")
	writeExecutable(t, diffHelper, "#!/bin/sh\nprintf invoked > \""+diffMarker+"\"\nexit 0\n")
	writeExecutable(t, fsmonitorHelper, "#!/bin/sh\nprintf invoked > \""+fsmonitorMarker+"\"\nexit 0\n")

	runGit(t, workspace, "config", "diff.external", diffHelper)
	runGit(t, workspace, "config", "diff.agent-controlled.textconv", diffHelper)
	runGit(t, workspace, "config", "core.fsmonitor", fsmonitorHelper)
	writeFile(t, workspace, "tracked.txt", "changed\n")

	diff, err := gitOutput(context.Background(), workspace, "diff", "--binary")
	if err != nil {
		t.Fatalf("hardened git diff: %v", err)
	}
	if len(diff) == 0 {
		t.Fatal("hardened git diff returned no tracked change")
	}
	if _, err := gitOutput(context.Background(), workspace, "diff", "--name-status", "-z", "--find-renames"); err != nil {
		t.Fatalf("hardened git name-status: %v", err)
	}
	if _, err := gitOutput(context.Background(), workspace, "ls-files", "--others", "--exclude-standard", "-z"); err != nil {
		t.Fatalf("hardened git ls-files: %v", err)
	}

	for name, marker := range map[string]string{
		"external diff/textconv": diffMarker,
		"fsmonitor":              fsmonitorMarker,
	} {
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s helper executed in server process", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspect %s marker: %v", name, err)
		}
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}
