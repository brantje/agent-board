package store

import (
	"context"
	"time"
)

// AuthPhase2Store extends the Phase 1 authentication store with the smallest
// user-administration, self-service session and settings operations required by
// issue #76. Keeping this separate preserves the Phase 1 AuthStore contract for
// existing focused test fakes and alternate implementations.
type AuthPhase2Store interface {
	ListUsers(context.Context) ([]User, error)
	UpdateUserIdentity(context.Context, string, string, string, string) (User, error)
	ListUserAuthSessions(context.Context, string, time.Time) ([]AuthSession, error)
	RevokeAuthSession(context.Context, string, string, time.Time) error
	RevokeOtherAuthSessions(context.Context, string, string, time.Time) error
	UpdateAuthSettings(context.Context, AuthSettings) (AuthSettings, error)
}
