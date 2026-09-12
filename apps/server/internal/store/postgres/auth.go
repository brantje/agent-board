package postgres

import (
	"context"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const userColumns = `id::text, username, email, display_name, COALESCE(password_hash,''), deployment_role, status, force_password_change, auth_version, created_at, updated_at`
const authSessionColumns = `id::text, user_id::text, refresh_token_hash, expires_at, revoked_at, created_at, last_used_at`
const passwordTokenColumns = `id::text, user_id::text, purpose, token_hash, expires_at, consumed_at, revoked_at, created_at`

func scanUser(row pgx.Row) (store.User, error) {
	var value store.User
	if err := row.Scan(
		&value.ID,
		&value.Username,
		&value.Email,
		&value.DisplayName,
		&value.PasswordHash,
		&value.DeploymentRole,
		&value.Status,
		&value.ForcePasswordChange,
		&value.AuthVersion,
		&value.CreatedAt,
		&value.UpdatedAt,
	); err != nil {
		return store.User{}, notFound(err)
	}
	return value, nil
}

func scanAuthSession(row pgx.Row) (store.AuthSession, error) {
	var value store.AuthSession
	if err := row.Scan(
		&value.ID,
		&value.UserID,
		&value.RefreshTokenHash,
		&value.ExpiresAt,
		&value.RevokedAt,
		&value.CreatedAt,
		&value.LastUsedAt,
	); err != nil {
		return store.AuthSession{}, notFound(err)
	}
	return value, nil
}

func scanPasswordToken(row pgx.Row) (store.PasswordToken, error) {
	var value store.PasswordToken
	if err := row.Scan(
		&value.ID,
		&value.UserID,
		&value.Purpose,
		&value.TokenHash,
		&value.ExpiresAt,
		&value.ConsumedAt,
		&value.RevokedAt,
		&value.CreatedAt,
	); err != nil {
		return store.PasswordToken{}, notFound(err)
	}
	return value, nil
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) BootstrapUser(ctx context.Context, input store.User) (store.User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Bootstrap is a deployment-global singleton decision. Serializing user
	// creation here makes the zero-users check and first insert one operation.
	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return store.User{}, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return store.User{}, err
	}
	if exists {
		return store.User{}, store.ErrConflict
	}
	if err := ensureLoginIdentifiersAvailable(ctx, tx, input.Username, input.Email); err != nil {
		return store.User{}, err
	}
	value, err := insertUser(ctx, tx, input)
	if err != nil {
		return store.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.User{}, err
	}
	return value, nil
}

func (s *Store) CreateUser(ctx context.Context, input store.User) (store.User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return store.User{}, err
	}
	if err := ensureLoginIdentifiersAvailable(ctx, tx, input.Username, input.Email); err != nil {
		return store.User{}, err
	}
	value, err := insertUser(ctx, tx, input)
	if err != nil {
		return store.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.User{}, err
	}
	return value, nil
}

func ensureLoginIdentifiersAvailable(ctx context.Context, tx pgx.Tx, username, email string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE username IN ($1,$2) OR email IN ($1,$2)
		)
	`, username, email).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return store.ErrConflict
	}
	return nil
}

func insertUser(ctx context.Context, tx pgx.Tx, input store.User) (store.User, error) {
	return scanUser(tx.QueryRow(ctx, `
		INSERT INTO users (username,email,display_name,password_hash,deployment_role,status,force_password_change,auth_version)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,CASE WHEN $8 < 1 THEN 1 ELSE $8 END)
		RETURNING `+userColumns,
		input.Username,
		input.Email,
		input.DisplayName,
		input.PasswordHash,
		input.DeploymentRole,
		input.Status,
		input.ForcePasswordChange,
		input.AuthVersion,
	))
}

func (s *Store) GetUser(ctx context.Context, id string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
}

func (s *Store) GetUserByLogin(ctx context.Context, login string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username=$1 OR email=$1`, login))
}

func (s *Store) SetUserStatus(ctx context.Context, id, status string) (store.User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current string
	if err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&current); err != nil {
		return store.User{}, notFound(err)
	}
	if current == status {
		value, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
		if err != nil {
			return store.User{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return store.User{}, err
		}
		return value, nil
	}
	value, err := scanUser(tx.QueryRow(ctx, `
		UPDATE users
		SET status=$2,auth_version=auth_version+1,updated_at=now()
		WHERE id=$1
		RETURNING `+userColumns, id, status))
	if err != nil {
		return store.User{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
		return store.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.User{}, err
	}
	return value, nil
}

func (s *Store) SetUserPassword(ctx context.Context, id, passwordHash string, forcePasswordChange bool) (store.User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	value, err := scanUser(tx.QueryRow(ctx, `
		UPDATE users
		SET password_hash=$2,
		    status=CASE WHEN status='pending' THEN 'active' ELSE status END,
		    force_password_change=$3,
		    auth_version=auth_version+1,
		    updated_at=now()
		WHERE id=$1
		RETURNING `+userColumns, id, passwordHash, forcePasswordChange))
	if err != nil {
		return store.User{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
		return store.User{}, err
	}
	if err := revokeOutstandingPasswordTokens(ctx, tx, id); err != nil {
		return store.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.User{}, err
	}
	return value, nil
}

func (s *Store) GetAuthSettings(ctx context.Context) (store.AuthSettings, error) {
	var accessSeconds, refreshSeconds int64
	var policy store.PasswordPolicy
	if err := s.pool.QueryRow(ctx, `
		SELECT access_token_lifetime_seconds,refresh_token_lifetime_seconds,password_minimum_length,
		       require_uppercase,require_lowercase,require_number,require_symbol
		FROM auth_settings WHERE singleton=true
	`).Scan(
		&accessSeconds,
		&refreshSeconds,
		&policy.MinimumLength,
		&policy.RequireUppercase,
		&policy.RequireLowercase,
		&policy.RequireNumber,
		&policy.RequireSymbol,
	); err != nil {
		return store.AuthSettings{}, notFound(err)
	}
	return store.AuthSettings{
		AccessTokenLifetime:  time.Duration(accessSeconds) * time.Second,
		RefreshTokenLifetime: time.Duration(refreshSeconds) * time.Second,
		PasswordPolicy:       policy,
	}, nil
}

func (s *Store) CreateAuthSession(ctx context.Context, input store.AuthSession) (store.AuthSession, error) {
	return scanAuthSession(s.pool.QueryRow(ctx, `
		INSERT INTO auth_sessions (user_id,refresh_token_hash,expires_at)
		VALUES ($1,$2,$3)
		RETURNING `+authSessionColumns, input.UserID, input.RefreshTokenHash, input.ExpiresAt))
}

func (s *Store) GetAuthSessionByRefreshHash(ctx context.Context, hash []byte) (store.AuthSession, error) {
	return scanAuthSession(s.pool.QueryRow(ctx, `SELECT `+authSessionColumns+` FROM auth_sessions WHERE refresh_token_hash=$1`, hash))
}

func (s *Store) RotateAuthSession(ctx context.Context, id string, oldHash, newHash []byte, expiresAt, now time.Time) (store.AuthSession, error) {
	return scanAuthSession(s.pool.QueryRow(ctx, `
		UPDATE auth_sessions
		SET refresh_token_hash=$3,expires_at=$4,last_used_at=$5
		WHERE id=$1 AND refresh_token_hash=$2 AND revoked_at IS NULL AND expires_at>$5
		RETURNING `+authSessionColumns, id, oldHash, newHash, expiresAt, now))
}

func (s *Store) RevokeAuthSessionByRefreshHash(ctx context.Context, hash []byte, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=$2
		WHERE refresh_token_hash=$1 AND revoked_at IS NULL
	`, hash, now)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) RevokeUserAuthSessions(ctx context.Context, userID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,$2)
		WHERE user_id=$1 AND revoked_at IS NULL
	`, userID, now)
	return notFound(err)
}

func (s *Store) CreatePasswordToken(ctx context.Context, input store.PasswordToken) (store.PasswordToken, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.PasswordToken{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		UPDATE password_tokens
		SET revoked_at=now()
		WHERE user_id=$1 AND purpose=$2 AND consumed_at IS NULL AND revoked_at IS NULL
	`, input.UserID, input.Purpose); err != nil {
		return store.PasswordToken{}, err
	}
	value, err := scanPasswordToken(tx.QueryRow(ctx, `
		INSERT INTO password_tokens (user_id,purpose,token_hash,expires_at)
		VALUES ($1,$2,$3,$4)
		RETURNING `+passwordTokenColumns, input.UserID, input.Purpose, input.TokenHash, input.ExpiresAt))
	if err != nil {
		return store.PasswordToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.PasswordToken{}, err
	}
	return value, nil
}

func (s *Store) CompletePasswordToken(ctx context.Context, hash []byte, purpose, passwordHash string, now time.Time) (store.User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	if err := tx.QueryRow(ctx, `
		UPDATE password_tokens
		SET consumed_at=$3
		WHERE token_hash=$1 AND purpose=$2 AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>$3
		RETURNING user_id::text
	`, hash, purpose, now).Scan(&userID); err != nil {
		return store.User{}, notFound(err)
	}

	query := `
		UPDATE users
		SET password_hash=$2,force_password_change=false,auth_version=auth_version+1,updated_at=$3
		WHERE id=$1
		RETURNING ` + userColumns
	if purpose == store.PasswordTokenPurposeSetup {
		query = `
			UPDATE users
			SET password_hash=$2,status='active',force_password_change=false,auth_version=auth_version+1,updated_at=$3
			WHERE id=$1 AND status='pending'
			RETURNING ` + userColumns
	}
	value, err := scanUser(tx.QueryRow(ctx, query, userID, passwordHash, now))
	if err != nil {
		return store.User{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE user_id=$1 AND revoked_at IS NULL`, userID, now); err != nil {
		return store.User{}, err
	}
	if err := revokeOutstandingPasswordTokens(ctx, tx, userID); err != nil {
		return store.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.User{}, err
	}
	return value, nil
}

func revokeOutstandingPasswordTokens(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE password_tokens
		SET revoked_at=now()
		WHERE user_id=$1 AND consumed_at IS NULL AND revoked_at IS NULL
	`, userID)
	return err
}

var _ store.AuthStore = (*Store)(nil)
