package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Git interface {
	ValidateBranch(context.Context, string) error
	Clone(context.Context, string, string, string) error
	CheckoutNewBranch(context.Context, string, string) error
	HeadRevision(context.Context, string) (string, error)
	CurrentBranch(context.Context, string) (string, error)
	IsRepository(context.Context, string) bool
}

type GitCLI struct {
	binary string
}

func NewGitCLI(binary string) (*GitCLI, error) {
	if strings.TrimSpace(binary) == "" {
		binary = "git"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("find git executable: %w", err)
	}
	return &GitCLI{binary: resolved}, nil
}

func (g *GitCLI) ValidateBranch(ctx context.Context, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("git branch must not be blank")
	}
	_, err := g.run(ctx, "check-ref-format", "--branch", branch)
	return err
}

func (g *GitCLI) Clone(ctx context.Context, source, destination, branch string) error {
	_, err := g.run(ctx, "clone", "--no-hardlinks", "--no-tags", "--single-branch", "--branch", branch, "--", source, destination)
	return err
}

func (g *GitCLI) CheckoutNewBranch(ctx context.Context, repositoryPath, branch string) error {
	_, err := g.run(ctx, "-C", repositoryPath, "checkout", "-b", branch)
	return err
}

func (g *GitCLI) HeadRevision(ctx context.Context, repositoryPath string) (string, error) {
	return g.run(ctx, "-C", repositoryPath, "rev-parse", "--verify", "HEAD^{commit}")
}

func (g *GitCLI) CurrentBranch(ctx context.Context, repositoryPath string) (string, error) {
	return g.run(ctx, "-C", repositoryPath, "symbolic-ref", "--quiet", "--short", "HEAD")
}

func (g *GitCLI) IsRepository(ctx context.Context, repositoryPath string) bool {
	_, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--show-toplevel")
	return err == nil
}

func (g *GitCLI) run(ctx context.Context, args ...string) (string, error) {
	hardened := []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "credential.helper=",
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.file.allow=user",
	}
	commandArgs := append(hardened, args...)
	cmd := exec.CommandContext(ctx, g.binary, commandArgs...)
	cmd.Env = hardenedGitEnv()
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		if message == "" {
			return "", fmt.Errorf("git %s: %w", commandName(args), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", commandName(args), err, message)
	}
	return strings.TrimSpace(output.String()), nil
}

func commandName(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-C" && i+1 < len(args) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return "command"
}

func hardenedGitEnv() []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "SSH_") || upper == "HOME" || upper == "PAGER" || upper == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, item)
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GIT_SSH_COMMAND=false",
		"GIT_PAGER=cat",
		"PAGER=cat",
	)
}
