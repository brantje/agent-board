package repository

import (
	"os"
	"strings"
)

// Settings exposes deployment repository defaults for Project configuration UIs.
type Settings struct {
	DefaultRepositoryPath string   `json:"defaultRepositoryPath"`
	RepositoryRoots       []string `json:"repositoryRoots"`
}

func SettingsFromEnv() Settings {
	roots := ParseRoots(os.Getenv("AGENT_BOARD_REPOSITORY_ROOTS"))
	defaultPath := strings.TrimSpace(os.Getenv("AGENT_BOARD_REPOSITORY_MOUNT_PATH"))
	if defaultPath == "" && len(roots) > 0 {
		defaultPath = roots[0]
	}
	return Settings{
		DefaultRepositoryPath: defaultPath,
		RepositoryRoots:       roots,
	}
}
