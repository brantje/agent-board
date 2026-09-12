package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

type authSentinelReader struct {
	mu   sync.Mutex
	next byte
}

func (r *authSentinelReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next == 0 {
		r.next = 1
	}
	for i := range p {
		p[i] = r.next
	}
	r.next++
	return len(p), nil
}

func reviewAuthService(t *testing.T, s *Store) *app.AuthService {
	t.Helper()
	service, err := app.NewAuthService(s, app.AuthServiceConfig{
		SigningKey: bytes.Repeat([]byte{31}, 32),
		Random:     &authSentinelReader{},
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return service
}

func TestSchemaRejectsCrossFieldUserLoginNamespaceCollisions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO users (username, email, display_name, deployment_role, status)
		VALUES ('alice', 'ops@example.com', 'Alice', 'member', 'pending')
	`); err != nil {
		t.Fatalf("insert first user: %v", err)
	}

	for _, statement := range []string{
		`INSERT INTO users (username, email, display_name, deployment_role, status) VALUES ('ops@example.com', 'bob@example.com', 'Bob', 'member', 'pending')`,
		`INSERT INTO users (username, email, display_name, deployment_role, status) VALUES ('bob', 'alice', 'Bob', 'member', 'pending')`,
	} {
		_, err := pool.Exec(ctx, statement)
		if err == nil {
			t.Fatal("database accepted a cross-field login identifier collision")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("cross-field collision error = %v, want SQLSTATE 23505", err)
		}
	}
}

func TestAuthServiceConcurrentRefreshAllowsExactlyOneRotation(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	service := reviewAuthService(t, s)
	ctx := context.Background()

	if _, err := service.Bootstrap(ctx, app.BootstrapRegistration{
		Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: "ValidPassword1!",
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	initial, err := service.Login(ctx, "admin", "ValidPassword1!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	type result struct {
		tokens app.AuthTokens
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tokens, err := service.Refresh(ctx, initial.RefreshToken)
			results <- result{tokens: tokens, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	var rotated app.AuthTokens
	for result := range results {
		if result.err == nil {
			successes++
			rotated = result.tokens
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent refresh results successes=%d failures=%d", successes, failures)
	}
	if rotated.RefreshToken == "" || rotated.RefreshToken == initial.RefreshToken {
		t.Fatalf("winning refresh did not rotate the token")
	}
	if _, err := service.Refresh(ctx, rotated.RefreshToken); err != nil {
		t.Fatalf("winning rotated refresh token is not usable: %v", err)
	}
}

func TestAuthRawSecretsStayOutOfDurableAndLoggableSinks(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	service := reviewAuthService(t, s)
	ctx := context.Background()

	loginPassword := "SENTINEL-Login-Password-9!"
	setupPassword := "SENTINEL-Setup-Password-8!"
	resetPassword := "SENTINEL-Reset-Password-7!"

	if _, err := service.Bootstrap(ctx, app.BootstrapRegistration{
		Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: loginPassword,
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	login, err := service.Login(ctx, "admin", loginPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	rotated, err := service.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	_, replayErr := service.Refresh(ctx, login.RefreshToken)
	if replayErr == nil {
		t.Fatal("replayed refresh token unexpectedly succeeded")
	}
	_, loginErr := service.Login(ctx, "missing-user", loginPassword)
	if loginErr == nil {
		t.Fatal("missing-user login unexpectedly succeeded")
	}

	pending, err := s.CreateUser(ctx, store.User{
		Username: "pending", Email: "pending@example.com", DisplayName: "Pending",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusPending, AuthVersion: 1,
	})
	if err != nil {
		t.Fatalf("create pending user: %v", err)
	}
	setup, err := service.CreatePasswordToken(ctx, pending.ID, store.PasswordTokenPurposeSetup)
	if err != nil {
		t.Fatalf("create setup token: %v", err)
	}
	if _, err := service.CompletePasswordToken(ctx, setup.Token, store.PasswordTokenPurposeSetup, setupPassword); err != nil {
		t.Fatalf("complete setup token: %v", err)
	}

	preReset, err := service.Login(ctx, "pending", setupPassword)
	if err != nil {
		t.Fatalf("login before reset: %v", err)
	}
	reset, err := service.CreatePasswordToken(ctx, pending.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, resetPassword); err != nil {
		t.Fatalf("complete reset token: %v", err)
	}
	_, resetReuseErr := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, resetPassword)
	if resetReuseErr == nil {
		t.Fatal("reset token reuse unexpectedly succeeded")
	}
	if _, err := service.AuthenticateAccessToken(ctx, preReset.AccessToken); err == nil {
		t.Fatal("pre-reset access token survived password reset")
	}
	if _, err := service.Refresh(ctx, preReset.RefreshToken); err == nil {
		t.Fatal("pre-reset refresh token survived password reset")
	}

	var durable strings.Builder
	for _, query := range []string{
		`SELECT COALESCE(string_agg(row_to_json(u)::text, E'\n'), '') FROM users u`,
		`SELECT COALESCE(string_agg(row_to_json(s)::text, E'\n'), '') FROM auth_sessions s`,
		`SELECT COALESCE(string_agg(row_to_json(p)::text, E'\n'), '') FROM password_tokens p`,
		`SELECT COALESCE(string_agg(row_to_json(e)::text, E'\n'), '') FROM events e`,
	} {
		var value string
		if err := pool.QueryRow(ctx, query).Scan(&value); err != nil {
			t.Fatalf("inspect durable auth sink: %v", err)
		}
		durable.WriteString(value)
		durable.WriteByte('\n')
	}

	loggable := strings.Join([]string{
		fmt.Sprint(replayErr), fmt.Sprint(loginErr), fmt.Sprint(resetReuseErr),
	}, "\n")
	secrets := []string{
		loginPassword,
		setupPassword,
		resetPassword,
		login.AccessToken,
		login.RefreshToken,
		rotated.AccessToken,
		rotated.RefreshToken,
		setup.Token,
		preReset.AccessToken,
		preReset.RefreshToken,
		reset.Token,
	}
	for _, secret := range secrets {
		if secret == "" {
			t.Fatal("test secret unexpectedly empty")
		}
		if strings.Contains(durable.String(), secret) {
			t.Fatalf("raw secret leaked into durable sink: %q", secret)
		}
		if strings.Contains(loggable, secret) {
			t.Fatalf("raw secret leaked into loggable error surface: %q", secret)
		}
	}
}

func TestResetTokenReuseFailsExplicitly(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	service := reviewAuthService(t, s)
	ctx := context.Background()

	user, err := service.Bootstrap(ctx, app.BootstrapRegistration{
		Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: "ValidPassword1!",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	reset, err := service.CreatePasswordToken(ctx, user.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, "ReplacementPassword2!"); err != nil {
		t.Fatalf("first reset completion: %v", err)
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, "ReplacementPassword3!"); err == nil {
		t.Fatal("second reset completion unexpectedly succeeded")
	}

	// Keep the test clock-independent while still proving the first completion
	// mutated durable auth state.
	stored, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AuthVersion < 2 || stored.PasswordHash == "" {
		t.Fatalf("reset did not update durable auth state: %#v", stored)
	}
}

var _ = time.Second
