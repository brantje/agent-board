package mcpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

type actorContextKey struct{}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || s == nil || s.services == nil || s.services.Auth == nil {
			writeAuthenticationFailure(w)
			return
		}
		actor, err := s.services.Auth.AuthenticateAccessToken(r.Context(), token)
		if err != nil {
			writeAuthenticationFailure(w)
			return
		}
		ctx := context.WithValue(r.Context(), actorContextKey{}, actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(value string) (string, bool) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

func actorFromContext(ctx context.Context) (app.AuthenticatedUser, error) {
	actor, ok := ctx.Value(actorContextKey{}).(app.AuthenticatedUser)
	if !ok || actor.ID == "" {
		return app.AuthenticatedUser{}, app.NewError("authentication_failed", "authentication failed", nil)
	}
	return actor, nil
}

func writeAuthenticationFailure(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("authentication failed\n"))
}
