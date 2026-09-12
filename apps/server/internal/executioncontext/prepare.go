package executioncontext

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type SecretRequest struct {
	ProviderCredentialEnv   string
	RedactAuthorizedSecrets bool
}

type Prepared struct {
	Safe             SafeContext
	Secrets          map[string]string
	RedactionValues  []string
	ReleaseRedaction func()
}

type RedactionRegistrar interface {
	Register(string, []string)
}

type RedactionReleaser interface {
	Release(string)
}

type Preparer struct {
	resolver       *Resolver
	secretResolver SecretResolver
	provenance     ProvenanceStore
	redaction      RedactionRegistrar
}

func NewPreparer(resolver *Resolver, secretResolver SecretResolver, provenance ProvenanceStore, registrars ...RedactionRegistrar) (*Preparer, error) {
	if resolver == nil || provenance == nil {
		return nil, fmt.Errorf("execution resolver and provenance store are required")
	}
	var registrar RedactionRegistrar
	if len(registrars) > 0 {
		registrar = registrars[0]
	}
	return &Preparer{resolver: resolver, secretResolver: secretResolver, provenance: provenance, redaction: registrar}, nil
}

func (p *Preparer) Prepare(ctx context.Context, projectID, runID string, request SecretRequest) (Prepared, error) {
	providerEnv := strings.TrimSpace(request.ProviderCredentialEnv)
	if providerEnv != "" && !validEnvName(providerEnv) {
		return Prepared{}, fail("execution_secret_target_invalid", "Provider credential environment target is invalid", nil)
	}

	resolved, err := p.resolver.Resolve(ctx, projectID, runID)
	if err != nil {
		return Prepared{}, err
	}
	includeProvider := providerEnv != "" || (request.RedactAuthorizedSecrets && resolved.ProviderCredentialRef != nil && strings.TrimSpace(*resolved.ProviderCredentialRef) != "")
	material, err := ResolveSecretMaterial(ctx, p.secretResolver, projectID, resolved, includeProvider)
	if err != nil {
		return Prepared{}, err
	}
	redactionValues := material.Values()

	executionSecrets := make(map[string]string, 1)
	if providerEnv != "" {
		executionSecrets[providerEnv] = string(material.ProviderCredential)
	}

	if err := EnsureProvenance(ctx, p.provenance, projectID, runID, resolved.Safe); err != nil {
		return Prepared{}, err
	}

	var releaseRedaction func()
	if p.redaction != nil && len(redactionValues) > 0 {
		p.redaction.Register(runID, redactionValues)
		if releaser, ok := p.redaction.(RedactionReleaser); ok {
			var once sync.Once
			releaseRedaction = func() {
				once.Do(func() { releaser.Release(runID) })
			}
		}
	}

	return Prepared{
		Safe:             resolved.Safe,
		Secrets:          executionSecrets,
		RedactionValues:  redactionValues,
		ReleaseRedaction: releaseRedaction,
	}, nil
}

func validEnvName(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		if index == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
