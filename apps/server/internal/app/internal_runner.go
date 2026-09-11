package app

import (
	"context"
	"errors"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// ErrInternalRunnerUnavailable means the optional host binary is missing.
// The control plane must keep serving; persistent external runners remain eligible.
var ErrInternalRunnerUnavailable = errors.New("internal agent-runner binary is unavailable")

func (s *RunnerService) prepareInternalRunner(ctx context.Context) (store.Runner, string, error) {
	runners, err := s.store.ListRunners(ctx)
	if err != nil {
		return store.Runner{}, "", err
	}
	token, hash, err := runnerCredential()
	if err != nil {
		return store.Runner{}, "", err
	}
	for _, r := range runners {
		if r.Internal {
			updated, err := s.store.RotateRunnerCredential(ctx, r.ID, hash)
			if err != nil {
				return store.Runner{}, "", err
			}
			return updated, token, nil
		}
	}
	r, err := s.store.CreateRunner(ctx, store.Runner{Name: "Internal runner", Internal: true, TokenHash: hash})
	if err != nil {
		return store.Runner{}, "", err
	}
	return r, token, nil
}

// SuperviseInternalRunner launches only agent-runner, never a coding Engine.
// Its child inherits host networking and a deliberately narrow environment.
func (s *RunnerService) SuperviseInternalRunner(ctx context.Context, binary, serverURL, workspaceRoot string) error {
	executable, err := exec.LookPath(binary)
	if err != nil {
		return ErrInternalRunnerUnavailable
	}
	for ctx.Err() == nil {
		r, token, err := s.prepareInternalRunner(ctx)
		if err != nil {
			return err
		}
		command := exec.CommandContext(ctx, executable)
		command.Env = internalRunnerEnvironment(serverURL, r.ID, token, workspaceRoot)
		command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
		command.WaitDelay = 5 * time.Second
		// Child output is intentionally not forwarded: credentials never enter the
		// server log pipeline through child startup diagnostics.
		_ = command.Run()
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}
func internalRunnerEnvironment(serverURL, id, token, root string) []string {
	values := []string{"AGENT_BOARD_URL=" + serverURL, "AGENT_RUNNER_ID=" + id, "AGENT_RUNNER_TOKEN=" + token, "AGENT_RUNNER_WORKSPACE_ROOT=" + root}
	for _, name := range []string{"PATH", "HOME", "LANG", "TMPDIR", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v, ok := os.LookupEnv(name); ok {
			values = append(values, name+"="+v)
		}
	}
	return values
}
