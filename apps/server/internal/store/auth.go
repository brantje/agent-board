package store

import (
	"context"
	"time"
)

const (
	DeploymentRoleAdmin  = "admin"
	DeploymentRoleMember = "member"

	UserStatusPending  = "pending"
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"

	PasswordTokenPurposeSetup = "setup"
	PasswordTokenPurposeReset = "reset"
)

type User struct {
	ID                  string
	Username            string
	Email               string
	DisplayName         string
	PasswordHash        string `json:"-"`
	DeploymentRole      string
	Status              string
	ForcePasswordChange bool
	AuthVersion         int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type AuthSession struct {
	ID               string
	UserID           string
	RefreshTokenHash []byte `json:"-"`
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
	LastUsedAt       *time.Time
}

type PasswordToken struct {
	ID         string
	UserID     string
	Purpose    string
	TokenHash  []byte `json:"-"`
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

type PasswordPolicy struct {
	MinimumLength    int
	RequireUppercase bool
	RequireLowercase bool
	RequireNumber    bool
	RequireSymbol    bool
}

type AuthSettings struct {
	AccessTokenLifetime  time.Duration
	RefreshTokenLifetime time.Duration
	PasswordPolicy       PasswordPolicy
}

type AuthStore interface {
	UserCount(context.Context) (int, error)
	BootstrapUser(context.Context, User) (User, error)
	CreateUser(context.Context, User) (User, error)
	GetUser(context.Context, string) (User, error)
	GetUserByLogin(context.Context, string) (User, error)
	SetUserStatus(context.Context, string, string) (User, error)
	SetUserPassword(context.Context, string, string, bool) (User, error)

	GetAuthSettings(context.Context) (AuthSettings, error)

	CreateAuthSession(context.Context, AuthSession) (AuthSession, error)
	GetAuthSessionByRefreshHash(context.Context, []byte) (AuthSession, error)
	RotateAuthSession(context.Context, string, []byte, []byte, time.Time, time.Time) (AuthSession, error)
	RevokeAuthSessionByRefreshHash(context.Context, []byte, time.Time) error
	RevokeUserAuthSessions(context.Context, string, time.Time) error

	CreatePasswordToken(context.Context, PasswordToken) (PasswordToken, error)
	CompletePasswordToken(context.Context, []byte, string, string, time.Time) (User, error)
	CreatePendingUserWithSetupToken(context.Context, User, PasswordToken) (User, PasswordToken, error)
	ListUsers(context.Context) ([]User, error)
	UpdateUserIdentity(context.Context, string, string, string, string) (User, error)
	SetUserPasswordIfAuthVersion(context.Context, string, int64, string, bool) (User, error)
	SetUserDisabled(context.Context, string, bool) (User, error)
	ListUserAuthSessions(context.Context, string, time.Time) ([]AuthSession, error)
	RevokeAuthSession(context.Context, string, string, time.Time) error
	RevokeOtherAuthSessions(context.Context, string, string, time.Time) error
	UpdateAuthSettings(context.Context, AuthSettings) (AuthSettings, error)
}
