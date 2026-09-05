//go:build !windows

package session

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	processTreeCleanupGrace = 200 * time.Millisecond
	processTreeCleanupPoll  = 10 * time.Millisecond
	processTreeKillWait     = time.Second
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

// cleanupProcessTree removes descendants that remain in the session's original
// process group after the root command exits. Runtime containment remains
// responsible for descendants that deliberately leave this process group.
func cleanupProcessTree(pid int) error {
	alive, err := processGroupHasLiveMembers(pid)
	if err != nil {
		return err
	}
	if !alive {
		return nil
	}
	if err := terminateProcessTree(pid); err != nil {
		return err
	}
	clean, err := waitForProcessGroupExit(pid, processTreeCleanupGrace)
	if err != nil || clean {
		return err
	}
	if err := killProcessTree(pid); err != nil {
		return err
	}
	clean, err = waitForProcessGroupExit(pid, processTreeKillWait)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New("process tree remained alive after forced cleanup")
	}
	return nil
}

func waitForProcessGroupExit(pid int, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		alive, err := processGroupHasLiveMembers(pid)
		if err != nil {
			return false, err
		}
		if !alive {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(processTreeCleanupPoll)
	}
}

func processGroupHasLiveMembers(pgid int) (bool, error) {
	if runtime.GOOS == "linux" {
		if alive, ok := linuxProcessGroupHasLiveMembers(pgid); ok {
			return alive, nil
		}
	}
	if err := syscall.Kill(-pgid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		if errors.Is(err, syscall.EPERM) {
			return true, nil
		}
		return false, fmt.Errorf("inspect process tree: %w", err)
	}
	return true, nil
}

// linuxProcessGroupHasLiveMembers returns ok=false when /proc is unavailable so
// callers can fall back to signal-0 probing. Zombies are not live work and must
// not keep a session slot occupied indefinitely when the runner is PID 1.
func linuxProcessGroupHasLiveMembers(pgid int) (alive bool, ok bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		endName := bytes.LastIndexByte(stat, ')')
		if endName < 0 || endName+1 >= len(stat) {
			continue
		}
		fields := strings.Fields(string(stat[endName+1:]))
		if len(fields) < 3 || fields[0] == "Z" {
			continue
		}
		group, err := strconv.Atoi(fields[2])
		if err == nil && group == pgid {
			return true, true
		}
	}
	return false, true
}
