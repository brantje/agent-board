package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSecretStoreKeepsScopesSeparate(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	projectA, err := s.CreateProject(ctx, store.Project{Name: "secret-project-a", RepositoryPath: "/repo/a"})
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	projectB, err := s.CreateProject(ctx, store.Project{Name: "secret-project-b", RepositoryPath: "/repo/b"})
	if err != nil {
		t.Fatalf("create project B: %v", err)
	}

	if _, err := s.PutSecret(ctx, secrets.Record{Ref: "provider-token", Ciphertext: []byte("global-ciphertext"), KeyVersion: 1}); err != nil {
		t.Fatalf("put global secret: %v", err)
	}
	if _, err := s.PutSecret(ctx, secrets.Record{ProjectID: &projectA.ID, Ref: "provider-token", Ciphertext: []byte("project-a-ciphertext"), KeyVersion: 1}); err != nil {
		t.Fatalf("put project secret: %v", err)
	}

	global, err := s.GetSecret(ctx, nil, "provider-token")
	if err != nil {
		t.Fatalf("get global secret: %v", err)
	}
	if string(global.Ciphertext) != "global-ciphertext" {
		t.Fatalf("global ciphertext = %q", global.Ciphertext)
	}
	projectScoped, err := s.GetSecret(ctx, &projectA.ID, "provider-token")
	if err != nil {
		t.Fatalf("get project secret: %v", err)
	}
	if string(projectScoped.Ciphertext) != "project-a-ciphertext" {
		t.Fatalf("project ciphertext = %q", projectScoped.Ciphertext)
	}
	if _, err := s.GetSecret(ctx, &projectB.ID, "provider-token"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("foreign project lookup error = %v, want ErrNotFound", err)
	}
}

func TestSecretStoreUpsertRotatesCiphertextWithoutChangingScope(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	project, err := s.CreateProject(ctx, store.Project{Name: "secret-rotation-project", RepositoryPath: "/repo"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	first, err := s.PutSecret(ctx, secrets.Record{ProjectID: &project.ID, Ref: "runtime-token", Ciphertext: []byte("ciphertext-v1"), KeyVersion: 1})
	if err != nil {
		t.Fatalf("put first secret: %v", err)
	}
	second, err := s.PutSecret(ctx, secrets.Record{ProjectID: &project.ID, Ref: "runtime-token", Ciphertext: []byte("ciphertext-v2"), KeyVersion: 2})
	if err != nil {
		t.Fatalf("rotate secret: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("secret ID changed across rotation: %s != %s", first.ID, second.ID)
	}
	if second.KeyVersion != 2 || string(second.Ciphertext) != "ciphertext-v2" {
		t.Fatalf("rotated record = %+v", second)
	}
}
