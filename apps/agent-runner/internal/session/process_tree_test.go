//go:build linux

package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKillTerminatesDescendantProcess(t *testing.T) {
	workspace := t.TempDir()
	manager := NewManagerWithWorkspace(1, workspace)
	s, err := manager.Start("tree", Request{Command: []string{"sh", "-c", "sleep 30 & echo $! > child.pid; wait"}})
	if err != nil {
		t.Fatal(err)
	}

	pidPath := filepath.Join(workspace, "child.pid")
	var childPID int
	waitFor(t, time.Second, func() bool {
		data, readErr := os.ReadFile(pidPath)
		if readErr != nil {
			return false
		}
		childPID, readErr = strconv.Atoi(strings.TrimSpace(string(data)))
		return readErr == nil && childPID > 0
	})

	if err := s.Kill(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := s.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Signaled {
		t.Fatalf("expected process tree to be killed, got %#v", result)
	}

	waitFor(t, 2*time.Second, func() bool { return processGoneOrZombie(childPID) })
}

func processGoneOrZombie(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	return len(fields) > 2 && fields[2] == "Z"
}
