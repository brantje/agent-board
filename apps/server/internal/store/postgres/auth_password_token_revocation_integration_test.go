package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestPasswordMutationRevokesOutstandingPasswordTokens(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	service := reviewAuthService(t, s)
	ctx := context.Background()

	admin, err := service.Bootstrap(ctx, app.BootstrapRegistration{
		Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: "ValidPassword1!",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	setup, err := service.CreatePasswordToken(ctx, admin.ID, store.PasswordTokenPurposeSetup)
	if err != nil {
		t.Fatalf("create setup token: %v", err)
	}
	reset, err := service.CreatePasswordToken(ctx, admin.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	if _, err := service.SetPassword(ctx, admin.ID, "ReplacementPassword2!", false); err != nil {
		t.Fatalf("set password: %v", err)
	}
	if _, err := service.CompletePasswordToken(ctx, setup.Token, store.PasswordTokenPurposeSetup, "ReplacementPassword3!"); err == nil {
		t.Fatal("setup token survived direct password mutation")
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, "ReplacementPassword3!"); err == nil {
		t.Fatal("reset token survived direct password mutation")
	}
}

func TestPasswordTokenCompletionRevokesOtherOutstandingPasswordTokens(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	service := reviewAuthService(t, s)
	ctx := context.Background()

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
	reset, err := service.CreatePasswordToken(ctx, pending.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	if _, err := service.CompletePasswordToken(ctx, setup.Token, store.PasswordTokenPurposeSetup, "InitialPassword1!"); err != nil {
		t.Fatalf("complete setup token: %v", err)
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, "ReplacementPassword2!"); err == nil {
		t.Fatal("reset token survived completion of another password token")
	}
}
