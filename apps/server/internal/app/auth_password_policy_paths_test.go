package app

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestPasswordPolicyAppliesToBootstrapSetAndTokenCompletionPaths(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	memory := newAuthMemory()
	memory.settings.PasswordPolicy = store.PasswordPolicy{
		MinimumLength:    12,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
		RequireSymbol:    true,
	}
	service := authTestService(t, memory, &now)
	ctx := context.Background()

	registration := BootstrapRegistration{
		Username: "admin", Email: "admin@example.com", DisplayName: "Admin", Password: "weak-password",
	}
	if _, err := service.Bootstrap(ctx, registration); err == nil {
		t.Fatal("bootstrap accepted password that violates shared policy")
	}
	registration.Password = "ValidPassword1!"
	admin, err := service.Bootstrap(ctx, registration)
	if err != nil {
		t.Fatalf("bootstrap with valid password: %v", err)
	}

	if _, err := service.SetPassword(ctx, admin.ID, "weak-password", false); err == nil {
		t.Fatal("SetPassword accepted password that violates shared policy")
	}

	pending, err := memory.CreateUser(ctx, store.User{
		Username: "pending", Email: "pending@example.com", DisplayName: "Pending",
		DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusPending, AuthVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := service.CreatePasswordToken(ctx, pending.ID, store.PasswordTokenPurposeSetup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePasswordToken(ctx, setup.Token, store.PasswordTokenPurposeSetup, "weak-password"); err == nil {
		t.Fatal("setup completion accepted password that violates shared policy")
	}

	reset, err := service.CreatePasswordToken(ctx, admin.ID, store.PasswordTokenPurposeReset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePasswordToken(ctx, reset.Token, store.PasswordTokenPurposeReset, "weak-password"); err == nil {
		t.Fatal("reset completion accepted password that violates shared policy")
	}
}
