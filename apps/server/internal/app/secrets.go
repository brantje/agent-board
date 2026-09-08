package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
)

type SecretWriter interface {
	Put(context.Context, secrets.Scope, string, []byte) (secrets.Metadata, error)
}

type SecretStore interface {
	SecretWriter
	Resolve(context.Context, secrets.Scope, string) ([]byte, error)
}

func SecretStoreFromWriter(writer SecretWriter) SecretStore {
	if writer == nil {
		return nil
	}
	store, _ := writer.(SecretStore)
	return store
}
