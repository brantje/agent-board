package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultGitCommandTimeout = 5 * time.Minute

type Git interface {
	ValidateBranch(context.Context, string) error
	Clone(context.Context, string, string, string) error
	CheckoutNewBranch(context.Context, string, string) error
	HeadRevision(context.Context, string) (string, error)
	CurrentBranch(context.Context, string) (string, error)
	OriginURL(context.Context, string) (string, error)
	IsRepository(context.Context, string) (bool, error)
}

type GitCLI struct {
	binary         string
	commandTimeout time.Duration
}

func NewGitCLI(binary string) (*GitCLI, error) {
	return NewGitCLIWithTimeout(binary, defaultGitCommandTimeout)
}

func NewGitCLIWithTimeout(binary string, commandTimeout time.Duration) (*GitCLI, error) {
	if commandTimeout <= 0 {
		return nil, fmt.Errorf("git command timeout must be positive")
	}
	if strings.TrimSpace(binary) == "" {
		binary = "git"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("find git executable: %w", err)
	}
	return &GitCLI{binary: resolved, commandTimeout: commandTimeout}, nil
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

func (g *GitCLI) OriginURL(ctx context.Context, repositoryPath string) (string, error) {
	return g.run(ctx, "-C", repositoryPath, "remote", "get-url", "origin")
}

func (g *GitCLI) IsRepository(ctx context.Context, repositoryPath string) (bool, error) {
	if _, err := os.Lstat(filepath.Join(repositoryPath, ".git")); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect git metadata: %w", err)
	}
	toplevel, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return false, err
	}
	requested, err := filepath.EvalSymlinks(repositoryPath)
	if err != nil {
		return false, fmt.Errorf("canonicalize repository path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(toplevel)
	if err != nil {
		return false, fmt.Errorf("canonicalize git top-level: %w", err)
	}
	return filepath.Clean(resolved) == filepath.Clean(requested), nil
}

func (g *GitCLI) run(ctx context.Context, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, g.commandTimeout)
	defer cancel()

	hardened := []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "credential.helper=",
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.file.allow=user",
	}
	commandArgs := append(hardened, args...)
	cmd := exec.CommandContext(commandCtx, g.binary, commandArgs...)
	cmd.Env = hardenedGitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if contextErr := commandCtx.Err(); contextErr != nil {
			return "", fmt.Errorf("git %s: %w", commandName(args), contextErr)
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			return "", fmt.Errorf("git %s: %w", commandName(args), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", commandName(args), err, message)
	}
	return strings.TrimSpace(stdout.String()), nil
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
