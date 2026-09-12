package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func authUser(username, email, status string) store.User {
	passwordHash := ""
	if status != store.UserStatusPending {
		passwordHash = "$argon2id$test"
	}
	return store.User{
		Username:       username,
		Email:          email,
		DisplayName:    username,
		PasswordHash:   passwordHash,
		DeploymentRole: store.DeploymentRoleMember,
		Status:         status,
		AuthVersion:    1,
	}
}

func TestAuthStoreBootstrapIsRaceSafe(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	const attempts = 8
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := authUser(fmt.Sprintf("admin%d", i), fmt.Sprintf("admin%d@example.com", i), store.UserStatusActive)
			user.DeploymentRole = store.DeploymentRoleAdmin
			_, err := s.BootstrapUser(ctx, user)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected bootstrap error: %v", err)
		}
	}
	if successes != 1 || conflicts != attempts-1 {
		t.Fatalf("bootstrap results successes=%d conflicts=%d", successes, conflicts)
	}
	count, err := s.UserCount(ctx)
	if err != nil || count != 1 {
		t.Fatalf("user count = %d, %v", count, err)
	}
}

func TestAuthStoreDefaultsAndLoginIdentifierUniqueness(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	settings, err := s.GetAuthSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.AccessTokenLifetime != time.Hour || settings.RefreshTokenLifetime != 30*24*time.Hour || settings.PasswordPolicy.MinimumLength != 12 {
		t.Fatalf("unexpected auth settings: %#v", settings)
	}

	first, err := s.CreateUser(ctx, authUser("alpha", "alpha@example.com", store.UserStatusPending))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetUserByLogin(ctx, first.Username); err != nil {
		t.Fatalf("username lookup: %v", err)
	}
	if _, err := s.GetUserByLogin(ctx, first.Email); err != nil {
		t.Fatalf("email lookup: %v", err)
	}

	_, err = s.CreateUser(ctx, authUser("alpha@example.com", "second@example.com", store.UserStatusPending))
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-field login identifier conflict = %v", err)
	}
	_, err = s.CreateUser(ctx, authUser(" Unnormalized ", "third@example.com", store.UserStatusPending))
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("unnormalized username = %v", err)
	}
}

func TestAuthStoreRefreshRotationRevocationAndIndependentSessions(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	user, err := s.BootstrapUser(ctx, func() store.User {
		u := authUser("admin", "admin@example.com", store.UserStatusActive)
		u.DeploymentRole = store.DeploymentRoleAdmin
		return u
	}())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	firstHash := make([]byte, 32)
	firstHash[0] = 1
	secondHash := make([]byte, 32)
	secondHash[0] = 2
	rotatedHash := make([]byte, 32)
	rotatedHash[0] = 3

	first, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: user.ID, RefreshTokenHash: firstHash, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: user.ID, RefreshTokenHash: secondHash, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rotated, err := s.RotateAuthSession(ctx, first.ID, firstHash, rotatedHash, first.ExpiresAt, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if rotated.LastUsedAt == nil || !rotated.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("unexpected rotated session: %#v", rotated)
	}
	if _, err := s.GetAuthSessionByRefreshHash(ctx, firstHash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old refresh hash lookup = %v", err)
	}
	if _, err := s.GetAuthSessionByRefreshHash(ctx, secondHash); err != nil {
		t.Fatalf("independent session lost: %v", err)
	}
	if err := s.RevokeAuthSessionByRefreshHash(ctx, secondHash, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	second, err := s.GetAuthSessionByRefreshHash(ctx, secondHash)
	if err != nil || second.RevokedAt == nil {
		t.Fatalf("revoked session = %#v, %v", second, err)
	}
	if _, err := s.RotateAuthSession(ctx, second.ID, secondHash, firstHash, second.ExpiresAt, now.Add(3*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked session rotated: %v", err)
	}
}

func TestAuthStorePasswordMutationAndDisableInvalidateSessions(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	user, err := s.BootstrapUser(ctx, func() store.User {
		u := authUser("admin", "admin@example.com", store.UserStatusActive)
		u.DeploymentRole = store.DeploymentRoleAdmin
		return u
	}())
	if err != nil {
		t.Fatal(err)
	}
	backupAdmin := authUser("backup-admin", "backup-admin@example.com", store.UserStatusActive)
	backupAdmin.DeploymentRole = store.DeploymentRoleAdmin
	if _, err := s.CreateUser(ctx, backupAdmin); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	hash := make([]byte, 32)
	hash[0] = 4
	session, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: user.ID, RefreshTokenHash: hash, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.SetUserPassword(ctx, user.ID, "$argon2id$new", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed.AuthVersion != user.AuthVersion+1 || !changed.ForcePasswordChange {
		t.Fatalf("password mutation did not invalidate auth: %#v", changed)
	}
	storedSession, err := s.GetAuthSessionByRefreshHash(ctx, hash)
	if err != nil || storedSession.ID != session.ID || storedSession.RevokedAt == nil {
		t.Fatalf("password mutation session = %#v, %v", storedSession, err)
	}

	hash2 := make([]byte, 32)
	hash2[0] = 5
	if _, err := s.CreateAuthSession(ctx, store.AuthSession{UserID: user.ID, RefreshTokenHash: hash2, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	disabled, err := s.SetUserStatus(ctx, user.ID, store.UserStatusDisabled)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != store.UserStatusDisabled || disabled.AuthVersion != changed.AuthVersion+1 {
		t.Fatalf("disable did not invalidate auth: %#v", disabled)
	}
	storedSession, err = s.GetAuthSessionByRefreshHash(ctx, hash2)
	if err != nil || storedSession.RevokedAt == nil {
		t.Fatalf("disable session = %#v, %v", storedSession, err)
	}
}

func TestAuthStorePasswordTokenReplacementExpiryAndSingleUse(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	admin := authUser("admin", "admin@example.com", store.UserStatusActive)
	admin.DeploymentRole = store.DeploymentRoleAdmin
	if _, err := s.BootstrapUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	pending, err := s.CreateUser(ctx, authUser("pending", "pending@example.com", store.UserStatusPending))
	if err != nil {
		t.Fatal(err)
	}
	issuedAt := time.Now().UTC()
	oldHash := make([]byte, 32)
	oldHash[0] = 6
	newHash := make([]byte, 32)
	newHash[0] = 7
	if _, err := s.CreatePasswordToken(ctx, store.PasswordToken{UserID: pending.ID, Purpose: store.PasswordTokenPurposeSetup, TokenHash: oldHash, ExpiresAt: issuedAt.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	replacement, err := s.CreatePasswordToken(ctx, store.PasswordToken{UserID: pending.ID, Purpose: store.PasswordTokenPurposeSetup, TokenHash: newHash, ExpiresAt: issuedAt.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	completionTime := replacement.CreatedAt.Add(time.Second)
	if _, err := s.CompletePasswordToken(ctx, oldHash, store.PasswordTokenPurposeSetup, "$argon2id$old", completionTime); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("replaced setup token completed: %v", err)
	}

	const attempts = 2
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CompletePasswordToken(ctx, newHash, store.PasswordTokenPurposeSetup, "$argon2id$new", completionTime)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	notFound := 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, store.ErrNotFound) {
			notFound++
		} else {
			t.Fatalf("unexpected completion error: %v", err)
		}
	}
	if successes != 1 || notFound != 1 {
		t.Fatalf("completion results successes=%d notFound=%d", successes, notFound)
	}
	activated, err := s.GetUser(ctx, pending.ID)
	if err != nil || activated.Status != store.UserStatusActive || activated.PasswordHash != "$argon2id$new" || activated.AuthVersion != 2 {
		t.Fatalf("activated user = %#v, %v", activated, err)
	}

	resetHash := make([]byte, 32)
	resetHash[0] = 8
	reset, err := s.CreatePasswordToken(ctx, store.PasswordToken{UserID: pending.ID, Purpose: store.PasswordTokenPurposeReset, TokenHash: resetHash, ExpiresAt: completionTime.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompletePasswordToken(ctx, resetHash, store.PasswordTokenPurposeReset, "$argon2id$reset", reset.ExpiresAt.Add(time.Second)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired reset token completed: %v", err)
	}
}
