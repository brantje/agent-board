//go:build windows

package session

import (
	"errors"
	"os"
	"os/exec"
)

func configureProcessTree(_ *exec.Cmd) {}

func terminateProcessTree(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(os.Interrupt)
}

func killProcessTree(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	err = process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func cleanupProcessTree(_ int) error {
	// v0.1 supports the Linux/Alpine runner target. Windows process-tree
	// containment remains intentionally outside this runtime contract.
	return nil
}
