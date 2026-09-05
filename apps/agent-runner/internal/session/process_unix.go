//go:build !windows

package session

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

func configureProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessTree(pid int) error {
	return signalProcessTree(pid, syscall.SIGTERM)
}

func killProcessTree(pid int) error {
	return signalProcessTree(pid, syscall.SIGKILL)
}

func signalProcessTree(pid int, signal syscall.Signal) error {
	if err := syscall.Kill(-pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal process tree: %w", err)
	}
	return nil
}
