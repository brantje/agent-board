package workspace

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

func TestGitCLIOperatesOnRepositoryOwnedByAnotherUser(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Skip("current user unavailable")
	}
	if current.Uid == "0" {
		t.Skip("cannot simulate cross-owner repository access as root")
	}

	baseGit := requireGit(t)
	parent := t.TempDir()
	source := createFixtureRepository(t, baseGit.GitCLI, parent)

	other := exec.Command("id", "-u", "nobody")
	output, err := other.Output()
	if err != nil {
		t.Skip("nobody user unavailable")
	}
	otherUID := string(output[:len(output)-1])
	if otherUID == current.Uid {
		t.Skip("nobody user matches current user")
	}
	if err := os.Chown(filepath.Join(source, ".git"), mustAtoi(otherUID), -1); err != nil {
		t.Skipf("cannot simulate cross-owner repository access: %v", err)
	}

	git, err := NewGitCLI("git")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := git.IsRepository(context.Background(), source)
	if err != nil {
		t.Fatalf("IsRepository() error = %v", err)
	}
	if !ok {
		t.Fatal("IsRepository() returned false for cross-owner repository")
	}
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
