package httpapi

import (
	"net/http"
	"strings"
)

func (a *api) deploymentGlobalAuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.auth == nil || !isDeploymentGlobalConfigurationPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := a.adminActor(w, r); !ok {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isDeploymentGlobalConfigurationPath(path string) bool {
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "/api/repository-settings" {
		return true
	}
	for _, prefix := range []string{
		"/api/providers",
		"/api/model-profiles",
		"/api/runtimes",
		"/api/agents",
		"/api/runners",
		"/api/secrets",
	} {
		if trimmed == prefix || strings.HasPrefix(trimmed, prefix+"/") {
			return true
		}
	}
	return false
}
