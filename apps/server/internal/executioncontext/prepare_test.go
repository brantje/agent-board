package executioncontext

import (
	"context"
	"testing"
)

func TestPrepareBuildsOnlyRequestedExecutionSecretsAndProvenance(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	secretResolver := &fakeSecretResolver{values: map[string][]byte{
		"provider-token": []byte("provider-plain"),
		"runtime-token":  []byte("runtime-plain"),
	}}
	provenance := &fakeProvenanceStore{}
	preparer, err := NewPreparer(resolver, secretResolver, provenance)
	if err != nil {
		t.Fatal(err)
	}

	prepared, err := preparer.Prepare(context.Background(), "p1", "r1", SecretRequest{
		ProviderCredentialEnv: "PROVIDER_TOKEN",
		RuntimeSecretRefs: map[string]string{
			"RUNTIME_TOKEN": "runtime-token",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.RuntimeID != "rt1" {
		t.Fatalf("runtime ID = %q", prepared.RuntimeID)
	}
	if prepared.Secrets["PROVIDER_TOKEN"] != "provider-plain" || prepared.Secrets["RUNTIME_TOKEN"] != "runtime-plain" {
		t.Fatalf("execution secrets = %+v", prepared.Secrets)
	}
	if len(prepared.Secrets) != 2 || len(prepared.RedactionValues) != 2 {
		t.Fatalf("prepared = %+v", prepared)
	}
	if provenance.puts != 1 {
		t.Fatalf("provenance writes = %d", provenance.puts)
	}
}

func TestPrepareRejectsUnauthorizedSecretBeforeProvenance(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	secretResolver := &fakeSecretResolver{values: map[string][]byte{"forged": []byte("plain")}}
	provenance := &fakeProvenanceStore{}
	preparer, err := NewPreparer(resolver, secretResolver, provenance)
	if err != nil {
		t.Fatal(err)
	}

	_, err = preparer.Prepare(context.Background(), "p1", "r1", SecretRequest{RuntimeSecretRefs: map[string]string{"TOKEN": "forged"}})
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_secret_unauthorized" {
		t.Fatalf("err = %#v", err)
	}
	if len(secretResolver.calls) != 0 {
		t.Fatalf("secret resolver calls = %v", secretResolver.calls)
	}
	if provenance.puts != 0 {
		t.Fatalf("provenance persisted for failed preflight")
	}
}

func TestPrepareRejectsInvalidAndDuplicateSecretTargets(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	preparer, err := NewPreparer(resolver, nil, &fakeProvenanceStore{})
	if err != nil {
		t.Fatal(err)
	}

	for _, request := range []SecretRequest{
		{ProviderCredentialEnv: "BAD-NAME"},
		{RuntimeSecretRefs: map[string]string{"1TOKEN": "runtime-token"}},
		{ProviderCredentialEnv: "TOKEN", RuntimeSecretRefs: map[string]string{"TOKEN": "runtime-token"}},
	} {
		if _, err := preparer.Prepare(context.Background(), "p1", "r1", request); err == nil {
			t.Fatalf("Prepare(%+v) unexpectedly succeeded", request)
		}
	}
}
