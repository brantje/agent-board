package app

import (
	"context"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *AuthService) setPasswordIfAuthVersion(ctx context.Context, userID string, expectedAuthVersion int64, password string, forcePasswordChange bool) (AuthenticatedUser, error) {
	settings, err := s.store.GetAuthSettings(ctx)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	if err := ValidatePassword(password, settings.PasswordPolicy); err != nil {
		return AuthenticatedUser{}, err
	}
	passwordHash, err := s.hashPassword(password)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	extended, err := s.phase2Store()
	if err != nil {
		return AuthenticatedUser{}, err
	}
	user, err := extended.SetUserPasswordIfAuthVersion(ctx, userID, expectedAuthVersion, passwordHash, forcePasswordChange)
	if errors.Is(err, store.ErrConflict) {
		return AuthenticatedUser{}, NewError("conflict", "authentication state changed", err)
	}
	if err != nil {
		return AuthenticatedUser{}, err
	}
	return publicUser(user), nil
}
