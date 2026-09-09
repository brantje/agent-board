package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
)

type stubSecretStore struct {
	stubSecretWriter
}

func (s *stubSecretStore) Resolve(context.Context, secrets.Scope, string) ([]byte, error) {
	return nil, secrets.ErrNotFound
}

type stubSecretWriter struct{}

func (stubSecretWriter) Put(context.Context, secrets.Scope, string, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}

func TestSecretStoreFromWriter(t *testing.T) {
	if SecretStoreFromWriter(nil) != nil {
		t.Fatal("expected nil for nil writer")
	}

	writerOnly := stubSecretWriter{}
	if SecretStoreFromWriter(writerOnly) != nil {
		t.Fatal("expected nil when writer does not implement SecretStore")
	}

	store := &stubSecretStore{}
	if got := SecretStoreFromWriter(store); got != store {
		t.Fatal("expected SecretStore implementation to be returned")
	}
}
