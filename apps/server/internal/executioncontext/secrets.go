package executioncontext

import (
	"context"
	"fmt"
	"strings"

	secretstore "github.com/brantje/agent-board/apps/server/internal/secrets"
)

type SecretResolver interface {
	Resolve(context.Context, secretstore.Scope, string) ([]byte, error)
}

type SecretMaterial struct {
	ProviderCredential []byte
}

func (m SecretMaterial) Values() []string {
	if len(m.ProviderCredential) == 0 {
		return nil
	}
	return []string{string(m.ProviderCredential)}
}

func ResolveSecretMaterial(ctx context.Context, resolver SecretResolver, projectID string, resolved Resolved, includeProviderCredential bool) (SecretMaterial, error) {
	if !includeProviderCredential {
		return SecretMaterial{}, nil
	}
	if resolver == nil {
		return SecretMaterial{}, fail("execution_secret_resolver_unavailable", "Execution secret resolver is unavailable", fmt.Errorf("secret resolver is not configured"))
	}
	if resolved.ProviderCredentialRef == nil || strings.TrimSpace(*resolved.ProviderCredentialRef) == "" {
		return SecretMaterial{}, fail("execution_provider_credential_unavailable", "Provider credential is unavailable for execution", nil)
	}

	scope := secretstore.Scope{ProjectID: &projectID}
	credential, err := resolver.Resolve(ctx, scope, *resolved.ProviderCredentialRef)
	if err != nil {
		return SecretMaterial{}, fail("execution_provider_credential_unavailable", "Provider credential is unavailable for execution", err)
	}
	return SecretMaterial{ProviderCredential: append([]byte(nil), credential...)}, nil
}
