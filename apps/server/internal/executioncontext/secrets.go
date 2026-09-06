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
	Runtime            map[string][]byte
}

func (m SecretMaterial) Values() []string {
	values := make([]string, 0, 1+len(m.Runtime))
	if len(m.ProviderCredential) > 0 {
		values = append(values, string(m.ProviderCredential))
	}
	for _, value := range m.Runtime {
		if len(value) > 0 {
			values = append(values, string(value))
		}
	}
	return values
}

func ResolveSecretMaterial(ctx context.Context, resolver SecretResolver, projectID string, resolved Resolved, requestedRuntimeRefs []string) (SecretMaterial, error) {
	if resolver == nil {
		return SecretMaterial{}, fmt.Errorf("secret resolver is required")
	}
	allowed := make(map[string]struct{}, len(resolved.AllowedSecretRefs))
	for _, ref := range resolved.AllowedSecretRefs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			allowed[ref] = struct{}{}
		}
	}

	requested := make([]string, 0, len(requestedRuntimeRefs))
	seen := make(map[string]struct{}, len(requestedRuntimeRefs))
	for _, ref := range requestedRuntimeRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return SecretMaterial{}, fail("execution_secret_unauthorized", "Runtime secret reference is not authorized for execution", nil)
		}
		if _, ok := allowed[ref]; !ok {
			return SecretMaterial{}, fail("execution_secret_unauthorized", "Runtime secret reference is not authorized for execution", nil)
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		requested = append(requested, ref)
	}

	scope := secretstore.Scope{ProjectID: &projectID}
	material := SecretMaterial{Runtime: make(map[string][]byte, len(requested))}
	if resolved.ProviderCredentialRef != nil {
		credential, err := resolver.Resolve(ctx, scope, *resolved.ProviderCredentialRef)
		if err != nil {
			return SecretMaterial{}, fail("execution_provider_credential_unavailable", "Provider credential is unavailable for execution", err)
		}
		material.ProviderCredential = append([]byte(nil), credential...)
	}
	for _, ref := range requested {
		value, err := resolver.Resolve(ctx, scope, ref)
		if err != nil {
			return SecretMaterial{}, fail("execution_secret_unavailable", "Runtime secret is unavailable for execution", err)
		}
		material.Runtime[ref] = append([]byte(nil), value...)
	}
	return material, nil
}
