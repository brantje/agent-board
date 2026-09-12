package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type bootstrapStatusResponse struct {
	Available bool `json:"available"`
}

type bootstrapRegistrationRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type passwordTokenCompletionRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type authUserResponse struct {
	ID                  string `json:"id"`
	Username            string `json:"username"`
	Email               string `json:"email"`
	DisplayName         string `json:"displayName"`
	DeploymentRole      string `json:"deploymentRole"`
	Status              string `json:"status"`
	ForcePasswordChange bool   `json:"forcePasswordChange"`
}

type authTokensResponse struct {
	AccessToken           string           `json:"accessToken"`
	AccessTokenExpiresAt  time.Time        `json:"accessTokenExpiresAt"`
	RefreshToken          string           `json:"refreshToken"`
	RefreshTokenExpiresAt time.Time        `json:"refreshTokenExpiresAt"`
	User                  authUserResponse `json:"user"`
}

func (a *api) registerAuthRoutes(r chi.Router) {
	r.Get("/auth/bootstrap", a.handleBootstrapStatus)
	r.Post("/auth/bootstrap/register", a.handleBootstrapRegister)
	r.Post("/auth/login", a.handleLogin)
	r.Post("/auth/refresh", a.handleRefresh)
	r.Post("/auth/logout", a.handleLogout)
	r.Post("/auth/setup/complete", a.handleSetupComplete)
	r.Post("/auth/reset/complete", a.handleResetComplete)
	r.Get("/auth/me", a.handleAuthMe)
}

func (a *api) handleBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	available, err := a.auth.BootstrapAvailable(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootstrapStatusResponse{Available: available})
}

func (a *api) handleBootstrapRegister(w http.ResponseWriter, r *http.Request) {
	var input bootstrapRegistrationRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	user, err := a.auth.Bootstrap(r.Context(), app.BootstrapRegistration{
		Username:    input.Username,
		Email:       input.Email,
		DisplayName: input.DisplayName,
		Password:    input.Password,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, authUserDTO(user))
}

func (a *api) handleLogin(w http.ResponseWriter, r *http.Request) {
	var input loginRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	tokens, err := a.auth.Login(r.Context(), input.Login, input.Password)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeSensitiveJSON(w, http.StatusOK, authTokensDTO(tokens))
}

func (a *api) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var input refreshRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	tokens, err := a.auth.Refresh(r.Context(), input.RefreshToken)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeSensitiveJSON(w, http.StatusOK, authTokensDTO(tokens))
}

func (a *api) handleLogout(w http.ResponseWriter, r *http.Request) {
	var input refreshRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.RefreshToken) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "refreshToken is required")
		return
	}
	if err := a.auth.Logout(r.Context(), input.RefreshToken); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	a.handlePasswordTokenCompletion(w, r, store.PasswordTokenPurposeSetup)
}

func (a *api) handleResetComplete(w http.ResponseWriter, r *http.Request) {
	a.handlePasswordTokenCompletion(w, r, store.PasswordTokenPurposeReset)
}

func (a *api) handlePasswordTokenCompletion(w http.ResponseWriter, r *http.Request, purpose string) {
	var input passwordTokenCompletionRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	user, err := a.auth.CompletePasswordToken(r.Context(), input.Token, purpose, input.Password)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, authUserDTO(user))
}

func (a *api) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return
	}
	user, err := a.auth.AuthenticateAccessToken(r.Context(), token)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, authUserDTO(user))
}

func bearerToken(r *http.Request) (string, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func authUserDTO(user app.AuthenticatedUser) authUserResponse {
	return authUserResponse{
		ID:                  user.ID,
		Username:            user.Username,
		Email:               user.Email,
		DisplayName:         user.DisplayName,
		DeploymentRole:      user.DeploymentRole,
		Status:              user.Status,
		ForcePasswordChange: user.ForcePasswordChange,
	}
}

func authTokensDTO(tokens app.AuthTokens) authTokensResponse {
	return authTokensResponse{
		AccessToken:           tokens.AccessToken,
		AccessTokenExpiresAt:  tokens.AccessTokenExpiresAt,
		RefreshToken:          tokens.RefreshToken,
		RefreshTokenExpiresAt: tokens.RefreshTokenExpiresAt,
		User:                  authUserDTO(tokens.User),
	}
}

func writeSensitiveJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, value)
}
