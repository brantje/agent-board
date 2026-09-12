package app

import (
	"context"
	"errors"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type PendingUserRegistration struct {
	Username    string
	Email       string
	DisplayName string
}

type PendingUserResult struct {
	User  AuthenticatedUser
	Setup PasswordTokenSecret
}

type UserProfileUpdate struct {
	Username    string
	Email       string
	DisplayName string
}

type AuthSessionInfo struct {
	ID         string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

func requireDeploymentAdmin(actor AuthenticatedUser) error {
	if actor.ID == "" || actor.DeploymentRole != store.DeploymentRoleAdmin || actor.Status != store.UserStatusActive {
		return NewError("forbidden", "deployment admin access is required", store.ErrInvalidArgument)
	}
	return nil
}

func (s *AuthService) phase2Store() (store.AuthPhase2Store, error) {
	extended, ok := s.store.(store.AuthPhase2Store)
	if !ok {
		return nil, NewError("auth_management_unavailable", "authentication management is unavailable", store.ErrInvalidArgument)
	}
	return extended, nil
}

func (s *AuthService) AuthenticateDeploymentAdmin(ctx context.Context, accessToken string) (AuthenticatedUser, error) {
	user, err := s.AuthenticateAccessToken(ctx, accessToken)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	if err := requireDeploymentAdmin(user); err != nil {
		return AuthenticatedUser{}, err
	}
	return user, nil
}

func (s *AuthService) ListUsers(ctx context.Context, actor AuthenticatedUser) ([]AuthenticatedUser, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return nil, err
	}
	extended, err := s.phase2Store()
	if err != nil {
		return nil, err
	}
	users, err := extended.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]AuthenticatedUser, 0, len(users))
	for _, user := range users {
		result = append(result, publicUser(user))
	}
	return result, nil
}

func (s *AuthService) CreatePendingUser(ctx context.Context, actor AuthenticatedUser, input PendingUserRegistration) (PendingUserResult, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return PendingUserResult{}, err
	}
	username, email, displayName, err := normalizeIdentity(input.Username, input.Email, input.DisplayName)
	if err != nil {
		return PendingUserResult{}, err
	}
	user, err := s.store.CreateUser(ctx, store.User{
		Username:       username,
		Email:          email,
		DisplayName:    displayName,
		DeploymentRole: store.DeploymentRoleMember,
		Status:         store.UserStatusPending,
		AuthVersion:    1,
	})
	if errors.Is(err, store.ErrConflict) {
		return PendingUserResult{}, NewError("conflict", "username or email is already in use", err)
	}
	if err != nil {
		return PendingUserResult{}, err
	}
	setup, err := s.CreatePasswordToken(ctx, user.ID, store.PasswordTokenPurposeSetup)
	if err != nil {
		return PendingUserResult{}, err
	}
	return PendingUserResult{User: publicUser(user), Setup: setup}, nil
}

func (s *AuthService) AdminCreatePasswordToken(ctx context.Context, actor AuthenticatedUser, userID, purpose string) (PasswordTokenSecret, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return PasswordTokenSecret{}, err
	}
	return s.CreatePasswordToken(ctx, userID, purpose)
}

func (s *AuthService) AdminSetPassword(ctx context.Context, actor AuthenticatedUser, userID, password string) (AuthenticatedUser, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return AuthenticatedUser{}, err
	}
	return s.SetPassword(ctx, userID, password, true)
}

func (s *AuthService) AdminSetDisabled(ctx context.Context, actor AuthenticatedUser, userID string, disabled bool) (AuthenticatedUser, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return AuthenticatedUser{}, err
	}
	status := store.UserStatusActive
	if disabled {
		status = store.UserStatusDisabled
	}
	return s.SetStatus(ctx, userID, status)
}

func (s *AuthService) UpdateOwnProfile(ctx context.Context, actor AuthenticatedUser, input UserProfileUpdate) (AuthenticatedUser, error) {
	username, email, displayName, err := normalizeIdentity(input.Username, input.Email, input.DisplayName)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	extended, err := s.phase2Store()
	if err != nil {
		return AuthenticatedUser{}, err
	}
	user, err := extended.UpdateUserIdentity(ctx, actor.ID, username, email, displayName)
	if errors.Is(err, store.ErrConflict) {
		return AuthenticatedUser{}, NewError("conflict", "username or email is already in use", err)
	}
	if err != nil {
		return AuthenticatedUser{}, err
	}
	return publicUser(user), nil
}

func (s *AuthService) ChangeOwnPassword(ctx context.Context, actor AuthenticatedUser, password string) (AuthenticatedUser, error) {
	return s.SetPassword(ctx, actor.ID, password, false)
}

func (s *AuthService) ListOwnSessions(ctx context.Context, actor AuthenticatedUser) ([]AuthSessionInfo, error) {
	extended, err := s.phase2Store()
	if err != nil {
		return nil, err
	}
	sessions, err := extended.ListUserAuthSessions(ctx, actor.ID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	result := make([]AuthSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, AuthSessionInfo{
			ID:         session.ID,
			ExpiresAt:  session.ExpiresAt,
			CreatedAt:  session.CreatedAt,
			LastUsedAt: session.LastUsedAt,
		})
	}
	return result, nil
}

func (s *AuthService) RevokeOwnSession(ctx context.Context, actor AuthenticatedUser, sessionID string) error {
	extended, err := s.phase2Store()
	if err != nil {
		return err
	}
	return extended.RevokeAuthSession(ctx, actor.ID, sessionID, s.now().UTC())
}

func (s *AuthService) LogoutOtherSessions(ctx context.Context, actor AuthenticatedUser, currentRefreshToken string) error {
	hash, err := hashOpaqueToken(currentRefreshToken)
	if err != nil {
		return authFailure()
	}
	current, err := s.store.GetAuthSessionByRefreshHash(ctx, hash)
	now := s.now().UTC()
	if err != nil || current.UserID != actor.ID || current.RevokedAt != nil || !current.ExpiresAt.After(now) {
		return authFailure()
	}
	extended, err := s.phase2Store()
	if err != nil {
		return err
	}
	return extended.RevokeOtherAuthSessions(ctx, actor.ID, current.ID, now)
}

func (s *AuthService) AuthSettings(ctx context.Context, actor AuthenticatedUser) (store.AuthSettings, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return store.AuthSettings{}, err
	}
	settings, err := s.store.GetAuthSettings(ctx)
	if err != nil {
		return store.AuthSettings{}, err
	}
	if err := validateAuthSettings(settings); err != nil {
		return store.AuthSettings{}, err
	}
	return settings, nil
}

func (s *AuthService) UpdateAuthSettings(ctx context.Context, actor AuthenticatedUser, settings store.AuthSettings) (store.AuthSettings, error) {
	if err := requireDeploymentAdmin(actor); err != nil {
		return store.AuthSettings{}, err
	}
	if err := validateAuthSettings(settings); err != nil {
		return store.AuthSettings{}, err
	}
	extended, err := s.phase2Store()
	if err != nil {
		return store.AuthSettings{}, err
	}
	return extended.UpdateAuthSettings(ctx, settings)
}
