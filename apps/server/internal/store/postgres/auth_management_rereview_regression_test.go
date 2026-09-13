package postgres

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAuthStoreCannotDisableLastActiveDeploymentAdmin(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	admin := authUser("admin", "admin@example.com", store.UserStatusActive)
	admin.DeploymentRole = store.DeploymentRoleAdmin
	admin.PasswordHash = "hash"
	admin, err := s.BootstrapUser(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetUserStatus(ctx, admin.ID, store.UserStatusDisabled); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("disable sole active admin error=%v want conflict", err)
	}
	stored, err := s.GetUser(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != store.UserStatusActive {
		t.Fatalf("sole active admin status=%q want active", stored.Status)
	}
}

func TestAuthStoreConcurrentDisableCannotRemoveLastTwoAdmins(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	first := authUser("admin-one", "admin-one@example.com", store.UserStatusActive)
	first.DeploymentRole = store.DeploymentRoleAdmin
	first.PasswordHash = "hash-one"
	first, err := s.BootstrapUser(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	second := authUser("admin-two", "admin-two@example.com", store.UserStatusActive)
	second.DeploymentRole = store.DeploymentRoleAdmin
	second.PasswordHash = "hash-two"
	second, err = s.CreateUser(ctx, second)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			<-start
			_, err := s.SetUserStatus(ctx, userID, store.UserStatusDisabled)
			errs <- err
		}(id)
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent disable error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent disables successes=%d conflicts=%d want 1/1", successes, conflicts)
	}
	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	activeAdmins := 0
	for _, user := range users {
		if user.DeploymentRole == store.DeploymentRoleAdmin && user.Status == store.UserStatusActive {
			activeAdmins++
		}
	}
	if activeAdmins != 1 {
		t.Fatalf("active admins=%d want 1", activeAdmins)
	}
}

func TestAuthPhase2CreatePendingUserAndSetupTokenRollBackTogether(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION fail_phase2_setup_token_insert() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced setup token insert failure';
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_phase2_setup_token_insert
		BEFORE INSERT ON password_tokens
		FOR EACH ROW EXECUTE FUNCTION fail_phase2_setup_token_insert();
	`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS fail_phase2_setup_token_insert ON password_tokens`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS fail_phase2_setup_token_insert()`)
	}()

	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	authService, err := app.NewAuthService(s, app.AuthServiceConfig{
		Now:        func() time.Time { return now },
		Random:     bytes.NewReader(bytes.Repeat([]byte{7}, 256)),
		SigningKey: bytes.Repeat([]byte{9}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := app.AuthenticatedUser{
		ID:             "admin",
		DeploymentRole: store.DeploymentRoleAdmin,
		Status:         store.UserStatusActive,
	}
	_, err = authService.CreatePendingUser(ctx, actor, app.PendingUserRegistration{
		Username: "atomic-member", Email: "atomic-member@example.com", DisplayName: "Atomic Member",
	})
	if err == nil {
		t.Fatal("create pending user unexpectedly succeeded while setup-token insert was forced to fail")
	}
	if _, err := s.GetUserByLogin(ctx, "atomic-member"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed atomic create left user behind: %v", err)
	}
}

func TestAuthPhase2UpdateUserIdentityMissingRowUsesStoreNotFound(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	_, err := s.UpdateUserIdentity(ctx, "00000000-0000-0000-0000-000000000999", "missing", "missing@example.com", "Missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing identity update error=%v want store.ErrNotFound", err)
	}
}
