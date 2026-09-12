package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type pendingUserRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

type pendingUserResponse struct {
	User                authUserResponse `json:"user"`
	SetupToken          string           `json:"setupToken"`
	SetupTokenExpiresAt time.Time        `json:"setupTokenExpiresAt"`
}

type passwordTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type passwordChangeRequest struct {
	Password string `json:"password"`
}

type profileUpdateRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

type authSessionResponse struct {
	ID         string     `json:"id"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type authSettingsResponse struct {
	AccessTokenLifetimeSeconds  int64 `json:"accessTokenLifetimeSeconds"`
	RefreshTokenLifetimeSeconds int64 `json:"refreshTokenLifetimeSeconds"`
	MinimumPasswordLength       int   `json:"minimumPasswordLength"`
	RequireUppercase            bool  `json:"requireUppercase"`
	RequireLowercase            bool  `json:"requireLowercase"`
	RequireNumber               bool  `json:"requireNumber"`
	RequireSymbol               bool  `json:"requireSymbol"`
}

func (a *api) registerAuthPhase2Routes(r chi.Router) {
	r.Get("/auth/users", a.handleListUsers)
	r.Post("/auth/users", a.handleCreatePendingUser)
	r.Post("/auth/users/{userID}/setup-token", a.handleAdminSetupToken)
	r.Post("/auth/users/{userID}/reset-token", a.handleAdminResetToken)
	r.Put("/auth/users/{userID}/password", a.handleAdminSetPassword)
	r.Post("/auth/users/{userID}/disable", a.handleAdminDisableUser)
	r.Post("/auth/users/{userID}/enable", a.handleAdminEnableUser)
	r.Patch("/auth/me", a.handleUpdateOwnProfile)
	r.Put("/auth/me/password", a.handleChangeOwnPassword)
	r.Get("/auth/me/sessions", a.handleListOwnSessions)
	r.Delete("/auth/me/sessions/{sessionID}", a.handleRevokeOwnSession)
	r.Post("/auth/me/sessions/logout-others", a.handleLogoutOtherSessions)
	r.Get("/auth/settings", a.handleAuthSettings)
	r.Put("/auth/settings", a.handleUpdateAuthSettings)
}

func (a *api) authActor(w http.ResponseWriter, r *http.Request) (app.AuthenticatedUser, bool) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return app.AuthenticatedUser{}, false
	}
	user, err := a.auth.AuthenticateAccessToken(r.Context(), token)
	if err != nil {
		writeAppError(w, err)
		return app.AuthenticatedUser{}, false
	}
	return user, true
}

func (a *api) adminActor(w http.ResponseWriter, r *http.Request) (app.AuthenticatedUser, bool) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return app.AuthenticatedUser{}, false
	}
	user, err := a.auth.AuthenticateDeploymentAdmin(r.Context(), token)
	if err != nil {
		if apiErr, ok := app.AsError(err); ok && apiErr.Code == "forbidden" {
			writeError(w, http.StatusForbidden, apiErr.Code, apiErr.Message)
			return app.AuthenticatedUser{}, false
		}
		writeAppError(w, err)
		return app.AuthenticatedUser{}, false
	}
	return user, true
}

func (a *api) handleListUsers(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok { return }
	users, err := a.auth.ListUsers(r.Context(), actor)
	if err != nil { writeAppError(w, err); return }
	response := make([]authUserResponse, 0, len(users))
	for _, user := range users { response = append(response, authUserDTO(user)) }
	writeJSON(w, http.StatusOK, response)
}

func (a *api) handleCreatePendingUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok { return }
	var input pendingUserRequest
	if !decodeJSON(w, r, &input) { return }
	result, err := a.auth.CreatePendingUser(r.Context(), actor, app.PendingUserRegistration{Username: input.Username, Email: input.Email, DisplayName: input.DisplayName})
	if err != nil { writeAppError(w, err); return }
	writeSensitiveJSON(w, http.StatusCreated, pendingUserResponse{User: authUserDTO(result.User), SetupToken: result.Setup.Token, SetupTokenExpiresAt: result.Setup.ExpiresAt})
}

func (a *api) handleAdminSetupToken(w http.ResponseWriter, r *http.Request) { a.handleAdminPasswordToken(w, r, store.PasswordTokenPurposeSetup) }
func (a *api) handleAdminResetToken(w http.ResponseWriter, r *http.Request) { a.handleAdminPasswordToken(w, r, store.PasswordTokenPurposeReset) }
func (a *api) handleAdminPasswordToken(w http.ResponseWriter, r *http.Request, purpose string) {
	actor, ok := a.adminActor(w, r)
	if !ok { return }
	userID, ok := pathUUID(w, r, "userID")
	if !ok { return }
	secret, err := a.auth.AdminCreatePasswordToken(r.Context(), actor, userID, purpose)
	if err != nil { writeAppError(w, err); return }
	writeSensitiveJSON(w, http.StatusCreated, passwordTokenResponse{Token: secret.Token, ExpiresAt: secret.ExpiresAt})
}

func (a *api) handleAdminSetPassword(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok { return }
	userID, ok := pathUUID(w, r, "userID")
	if !ok { return }
	var input passwordChangeRequest
	if !decodeJSON(w, r, &input) { return }
	user, err := a.auth.AdminSetPassword(r.Context(), actor, userID, input.Password)
	if err != nil { writeAppError(w, err); return }
	writeJSON(w, http.StatusOK, authUserDTO(user))
}

func (a *api) handleAdminDisableUser(w http.ResponseWriter, r *http.Request) { a.handleAdminSetDisabled(w, r, true) }
func (a *api) handleAdminEnableUser(w http.ResponseWriter, r *http.Request) { a.handleAdminSetDisabled(w, r, false) }
func (a *api) handleAdminSetDisabled(w http.ResponseWriter, r *http.Request, disabled bool) {
	actor, ok := a.adminActor(w, r)
	if !ok { return }
	userID, ok := pathUUID(w, r, "userID")
	if !ok { return }
	user, err := a.auth.AdminSetDisabled(r.Context(), actor, userID, disabled)
	if err != nil { writeAppError(w, err); return }
	writeJSON(w, http.StatusOK, authUserDTO(user))
}

func (a *api) handleUpdateOwnProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authActor(w, r)
	if !ok { return }
	var input profileUpdateRequest
	if !decodeJSON(w, r, &input) { return }
	user, err := a.auth.UpdateOwnProfile(r.Context(), actor, app.UserProfileUpdate{Username: input.Username, Email: input.Email, DisplayName: input.DisplayName})
	if err != nil { writeAppError(w, err); return }
	writeJSON(w, http.StatusOK, authUserDTO(user))
}

func (a *api) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authActor(w, r)
	if !ok { return }
	var input passwordChangeRequest
	if !decodeJSON(w, r, &input) { return }
	user, err := a.auth.ChangeOwnPassword(r.Context(), actor, input.Password)
	if err != nil { writeAppError(w, err); return }
	writeSensitiveJSON(w, http.StatusOK, authUserDTO(user))
}

func (a *api) handleListOwnSessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authActor(w, r)
	if !ok { return }
	sessions, err := a.auth.ListOwnSessions(r.Context(), actor)
	if err != nil { writeAppError(w, err); return }
	response := make([]authSessionResponse, 0, len(sessions))
	for _, session := range sessions { response = append(response, authSessionResponse{ID: session.ID, ExpiresAt: session.ExpiresAt, CreatedAt: session.CreatedAt, LastUsedAt: session.LastUsedAt}) }
	writeSensitiveJSON(w, http.StatusOK, response)
}

func (a *api) handleRevokeOwnSession(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authActor(w, r)
	if !ok { return }
	sessionID, ok := pathUUID(w, r, "sessionID")
	if !ok { return }
	if err := a.auth.RevokeOwnSession(r.Context(), actor, sessionID); err != nil {
		if errors.Is(err, store.ErrNotFound) { writeError(w, http.StatusNotFound, "session_not_found", "session not found"); return }
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) handleLogoutOtherSessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authActor(w, r)
	if !ok { return }
	var input refreshRequest
	if !decodeJSON(w, r, &input) { return }
	if err := a.auth.LogoutOtherSessions(r.Context(), actor, input.RefreshToken); err != nil { writeAppError(w, err); return }
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) handleAuthSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok { return }
	settings, err := a.auth.AuthSettings(r.Context(), actor)
	if err != nil { writeAppError(w, err); return }
	writeJSON(w, http.StatusOK, authSettingsDTO(settings))
}

func (a *api) handleUpdateAuthSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.adminActor(w, r)
	if !ok { return }
	var input authSettingsResponse
	if !decodeJSON(w, r, &input) { return }
	if input.AccessTokenLifetimeSeconds < 300 || input.AccessTokenLifetimeSeconds > 86400 || input.RefreshTokenLifetimeSeconds < 3600 || input.RefreshTokenLifetimeSeconds > 31536000 {
		writeError(w, http.StatusBadRequest, "auth_settings_invalid", "token lifetime is outside the supported range")
		return
	}
	settings, err := a.auth.UpdateAuthSettings(r.Context(), actor, store.AuthSettings{
		AccessTokenLifetime: time.Duration(input.AccessTokenLifetimeSeconds) * time.Second,
		RefreshTokenLifetime: time.Duration(input.RefreshTokenLifetimeSeconds) * time.Second,
		PasswordPolicy: store.PasswordPolicy{MinimumLength: input.MinimumPasswordLength, RequireUppercase: input.RequireUppercase, RequireLowercase: input.RequireLowercase, RequireNumber: input.RequireNumber, RequireSymbol: input.RequireSymbol},
	})
	if err != nil { writeAppError(w, err); return }
	writeJSON(w, http.StatusOK, authSettingsDTO(settings))
}

func authSettingsDTO(settings store.AuthSettings) authSettingsResponse {
	return authSettingsResponse{
		AccessTokenLifetimeSeconds: int64(settings.AccessTokenLifetime / time.Second),
		RefreshTokenLifetimeSeconds: int64(settings.RefreshTokenLifetime / time.Second),
		MinimumPasswordLength: settings.PasswordPolicy.MinimumLength,
		RequireUppercase: settings.PasswordPolicy.RequireUppercase,
		RequireLowercase: settings.PasswordPolicy.RequireLowercase,
		RequireNumber: settings.PasswordPolicy.RequireNumber,
		RequireSymbol: settings.PasswordPolicy.RequireSymbol,
	}
}
