package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
)

type SecretWriter interface {
	Put(context.Context, secrets.Scope, string, []byte) (secrets.Metadata, error)
}
