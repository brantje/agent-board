package executioncontext

import (
	"context"
	"fmt"
	"strings"
)

type SecretRequest struct {
	ProviderCredentialEnv string
	RuntimeSecretRefs     map[string]string
}

type Prepared struct {
	Safe            SafeContext
	RuntimeID       string
	Secrets         map[string]string
	RedactionValues []string
}

type RedactionRegistrar interface {
	Register(string, []string)
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

	requestedRefs := make([]string, 0, len(request.RuntimeSecretRefs))
	for envName, ref := range request.RuntimeSecretRefs {
		if !validEnvName(envName) {
			return Prepared{}, fail("execution_secret_target_invalid", "Runtime secret environment target is invalid", nil)
		}
		if providerEnv != "" && envName == providerEnv {
			return Prepared{}, fail("execution_secret_target_invalid", "Execution secret environment targets must be unique", nil)
		}
		requestedRefs = append(requestedRefs, ref)
	}

	resolved, err := p.resolver.Resolve(ctx, projectID, runID)
	if err != nil {
		return Prepared{}, err
	}
	material, err := ResolveSecretMaterial(ctx, p.secretResolver, projectID, resolved, providerEnv != "", requestedRefs)
	if err != nil {
		return Prepared{}, err
	}
	redactionValues := material.Values()
	if p.redaction != nil {
		p.redaction.Register(runID, redactionValues)
	}
	if err := EnsureProvenance(ctx, p.provenance, projectID, runID, resolved.Safe); err != nil {
		return Prepared{}, err
	}

	executionSecrets := make(map[string]string, len(request.RuntimeSecretRefs)+1)
	if providerEnv != "" {
		executionSecrets[providerEnv] = string(material.ProviderCredential)
	}
	for envName, ref := range request.RuntimeSecretRefs {
		value, ok := material.Runtime[strings.TrimSpace(ref)]
		if !ok {
			return Prepared{}, fail("execution_secret_unavailable", "Runtime secret is unavailable for execution", nil)
		}
		executionSecrets[envName] = string(value)
	}

	return Prepared{
		Safe:            resolved.Safe,
		RuntimeID:       resolved.Safe.Runtime.ID,
		Secrets:         executionSecrets,
		RedactionValues: redactionValues,
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
