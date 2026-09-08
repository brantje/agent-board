package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultRuntimeUID = 100
	defaultRuntimeGID = 101
)

type RuntimeIdentity struct {
	UID int
	GID int
}

func RuntimeIdentityFromEnv() RuntimeIdentity {
	uid, err := strconv.Atoi(os.Getenv("AGENT_BOARD_RUNTIME_UID"))
	if err != nil || uid < 0 {
		uid = defaultRuntimeUID
	}
	gid, err := strconv.Atoi(os.Getenv("AGENT_BOARD_RUNTIME_GID"))
	if err != nil || gid < 0 {
		gid = defaultRuntimeGID
	}
	return RuntimeIdentity{UID: uid, GID: gid}
}

func PreparePublishedWorkspace(path string, identity RuntimeIdentity) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("prepare published workspace: path is required")
	}
	if identity.UID < 0 {
		return fmt.Errorf("prepare published workspace: runtime uid is required")
	}
	if identity.GID < 0 {
		return fmt.Errorf("prepare published workspace: runtime gid is required")
	}
	if os.Getuid() == 0 {
		return preparePublishedWorkspaceAsRoot(path, identity)
	}
	return ensurePublishedWorkspaceModes(path)
}

func preparePublishedWorkspaceAsRoot(path string, identity RuntimeIdentity) error {
	return filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %s: %w", current, err)
		}
		mode := publishedEntryMode(info, entry.IsDir())
		if err := os.Chown(current, identity.UID, identity.GID); err != nil {
			return fmt.Errorf("chown %s: %w", current, err)
		}
		if err := os.Chmod(current, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", current, err)
		}
		return nil
	})
}

func ensurePublishedWorkspaceModes(path string) error {
	return filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %s: %w", current, err)
		}
		mode := publishedEntryMode(info, entry.IsDir())
		if info.Mode().Perm() == mode {
			return nil
		}
		if err := os.Chmod(current, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", current, err)
		}
		return nil
	})
}

func publishedEntryMode(info fs.FileInfo, isDir bool) fs.FileMode {
	mode := info.Mode().Perm()
	if !isDir {
		return mode
	}
	return (mode | 0o755) & 0o777
}
