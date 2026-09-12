package postgres

import (
	"context"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreatePendingUserWithSetupToken(ctx context.Context, input store.User, token store.PasswordToken) (store.User, store.PasswordToken, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.User{}, store.PasswordToken{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return store.User{}, store.PasswordToken{}, err
	}
	if err := ensureLoginIdentifiersAvailable(ctx, tx, input.Username, input.Email); err != nil {
		return store.User{}, store.PasswordToken{}, err
	}
	user, err := insertUser(ctx, tx, input)
	if err != nil {
		return store.User{}, store.PasswordToken{}, err
	}
	token.UserID = user.ID
	storedToken, err := scanPasswordToken(tx.QueryRow(ctx, `
		INSERT INTO password_tokens (user_id,purpose,token_hash,expires_at)
		VALUES ($1,$2,$3,$4)
		RETURNING `+passwordTokenColumns, token.UserID, token.Purpose, token.TokenHash, token.ExpiresAt))
	if err != nil {
		return store.User{}, store.PasswordToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.User{}, store.PasswordToken{}, err
	}
	return user, storedToken, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]store.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]store.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func (s *Store) UpdateUserIdentity(ctx context.Context, id, username, email, displayName string) (store.User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return store.User{}, err
	}
	var collision bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id<>$1 AND (username IN ($2,$3) OR email IN ($2,$3))
		)
	`, id, username, email).Scan(&collision); err != nil {
		return store.User{}, err
	}
	if collision {
		return store.User{}, store.ErrConflict
	}
	user, err := scanUser(tx.QueryRow(ctx, `
		UPDATE users
		SET username=$2,email=$3,display_name=$4,updated_at=now()
		WHERE id=$1
		RETURNING `+userColumns, id, username, email, displayName))
	if err != nil {
		return store.User{}, notFound(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return store.User{}, err
	}
	return user, nil
}

func (s *Store) ListUserAuthSessions(ctx context.Context, userID string, now time.Time) ([]store.AuthSession, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+authSessionColumns+`
		FROM auth_sessions
		WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>$2
		ORDER BY COALESCE(last_used_at,created_at) DESC,created_at DESC,id
	`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]store.AuthSession, 0)
	for rows.Next() {
		session, err := scanAuthSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *Store) RevokeAuthSession(ctx context.Context, userID, sessionID string, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=$3
		WHERE user_id=$1 AND id=$2 AND revoked_at IS NULL
	`, userID, sessionID, now)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) RevokeOtherAuthSessions(ctx context.Context, userID, keepSessionID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=$3
		WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL
	`, userID, keepSessionID, now)
	return notFound(err)
}

func (s *Store) UpdateAuthSettings(ctx context.Context, settings store.AuthSettings) (store.AuthSettings, error) {
	accessSeconds := int64(settings.AccessTokenLifetime / time.Second)
	refreshSeconds := int64(settings.RefreshTokenLifetime / time.Second)
	var storedAccess, storedRefresh int64
	var policy store.PasswordPolicy
	if err := s.pool.QueryRow(ctx, `
		UPDATE auth_settings
		SET access_token_lifetime_seconds=$1,
		    refresh_token_lifetime_seconds=$2,
		    password_minimum_length=$3,
		    require_uppercase=$4,
		    require_lowercase=$5,
		    require_number=$6,
		    require_symbol=$7,
		    updated_at=now()
		WHERE singleton=true
		RETURNING access_token_lifetime_seconds,refresh_token_lifetime_seconds,password_minimum_length,
		          require_uppercase,require_lowercase,require_number,require_symbol
	`, accessSeconds, refreshSeconds, settings.PasswordPolicy.MinimumLength,
		settings.PasswordPolicy.RequireUppercase, settings.PasswordPolicy.RequireLowercase,
		settings.PasswordPolicy.RequireNumber, settings.PasswordPolicy.RequireSymbol,
	).Scan(&storedAccess, &storedRefresh, &policy.MinimumLength,
		&policy.RequireUppercase, &policy.RequireLowercase, &policy.RequireNumber, &policy.RequireSymbol); err != nil {
		return store.AuthSettings{}, notFound(err)
	}
	return store.AuthSettings{
		AccessTokenLifetime:  time.Duration(storedAccess) * time.Second,
		RefreshTokenLifetime: time.Duration(storedRefresh) * time.Second,
		PasswordPolicy:       policy,
	}, nil
}

var _ store.AuthPhase2Store = (*Store)(nil)
