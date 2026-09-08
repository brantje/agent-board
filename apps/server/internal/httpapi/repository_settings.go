package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/repository"
)

func (a *api) getRepositorySettings(w http.ResponseWriter, _ *http.Request) {
	settings := a.repositorySettings
	if settings.DefaultRepositoryPath == "" && len(settings.RepositoryRoots) == 0 {
		settings = repository.SettingsFromEnv()
	}
	writeJSON(w, http.StatusOK, settings)
}
