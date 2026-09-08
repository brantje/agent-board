package evidence

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

func TestHardenedGitConfigAllowsBindMountedRepositories(t *testing.T) {
	config := hardenedGitConfig()
	for i := 0; i < len(config); i++ {
		if config[i] == "safe.directory=*" {
			return
		}
	}
	t.Fatal("hardenedGitConfig() missing safe.directory=*")
}

func TestGitOutputStagedDiffOnRepositoryOwnedByAnotherUser(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Skip("current user unavailable")
	}

	workspace := t.TempDir()
	runGit(t, workspace, "init")
	runGit(t, workspace, "config", "user.email", "test@example.com")
	runGit(t, workspace, "config", "user.name", "Test")
	writeFile(t, workspace, "tracked.txt", "base\n")
	runGit(t, workspace, "add", ".")
	runGit(t, workspace, "commit", "-m", "base")
	writeFile(t, workspace, "tracked.txt", "changed\n")
	runGit(t, workspace, "add", "tracked.txt")

	otherUID := crossOwnerUID(t, current.Uid)
	if err := chownRepository(workspace, otherUID); err != nil {
		t.Skipf("cannot simulate cross-owner repository access: %v", err)
	}

	_, err = gitOutput(context.Background(), workspace, "diff", "--name-status", "-z", "--find-renames", "--cached", "HEAD")
	if err != nil {
		t.Fatalf("gitOutput() staged diff error = %v", err)
	}
}

func crossOwnerUID(t *testing.T, currentUID string) int {
	t.Helper()
	if currentUID == "0" {
		return 100
	}
	other := exec.Command("id", "-u", "nobody")
	output, err := other.Output()
	if err != nil {
		t.Skip("nobody user unavailable")
	}
	otherUID := mustAtoi(string(output[:len(output)-1]))
	if otherUID < 0 {
		t.Skip("nobody uid unavailable")
	}
	if string(output[:len(output)-1]) == currentUID {
		t.Skip("nobody user matches current user")
	}
	return otherUID
}

func chownRepository(workspace string, uid int) error {
	if err := os.Chown(workspace, uid, -1); err != nil {
		return err
	}
	return os.Chown(filepath.Join(workspace, ".git"), uid, -1)
}

func mustAtoi(value string) int {
	var parsed int
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return -1
		}
		parsed = parsed*10 + int(digit-'0')
	}
	return parsed
}
