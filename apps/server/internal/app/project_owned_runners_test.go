package app

import (
	"context"
	"testing"
)

func TestProjectOwnedRunnerLifecycleKeepsServerAuthoritativeOwner(t *testing.T) {
	ctx := context.Background()
	memory := &runnerMemory{}
	service := NewRunnerService(memory)

	pending, registrationToken, err := service.CreateForProject(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if pending.ProjectID == nil || *pending.ProjectID != "project-1" {
		t.Fatalf("pending owner=%v", pending.ProjectID)
	}
	registered, _, err := service.Register(ctx, registrationToken, "project-host")
	if err != nil {
		t.Fatal(err)
	}
	if registered.ProjectID == nil || *registered.ProjectID != "project-1" {
		t.Fatalf("registration changed owner=%v", registered.ProjectID)
	}
	if _, err := service.UpdateForProject(ctx, "project-2", registered.ID, "wrong", nil); err == nil {
		t.Fatal("foreign project updated owned runner")
	}
	updated, err := service.UpdateForProject(ctx, "project-1", registered.ID, "owned host", nil)
	if err != nil || updated.Name != "owned host" {
		t.Fatalf("owner update=%+v err=%v", updated, err)
	}
	if _, _, err := service.RotateForProject(ctx, "project-2", registered.ID); err == nil {
		t.Fatal("foreign project rotated owned runner")
	}
	if _, err := service.RevokeForProject(ctx, "project-2", registered.ID, false); err == nil {
		t.Fatal("foreign project revoked owned runner")
	}
	if _, err := service.RevokeForProject(ctx, "project-1", registered.ID, false); err != nil {
		t.Fatalf("owner revoke: %v", err)
	}
}
