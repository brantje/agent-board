package executioncontext

import (
	"context"
	"testing"

	secretstore "github.com/brantje/agent-board/apps/server/internal/secrets"
)

type fakeSecretResolver struct {
	calls  []string
	values map[string][]byte
}

func (f *fakeSecretResolver) Resolve(_ context.Context, _ secretstore.Scope, ref string) ([]byte, error) {
	f.calls = append(f.calls, ref)
	return append([]byte(nil), f.values[ref]...), nil
}

func TestProviderCredentialResolvesWhenRequested(t *testing.T) {
	credentialRef := "provider-token"
	resolver := &fakeSecretResolver{values: map[string][]byte{
		"provider-token": []byte("provider-plain"),
	}}
	material, err := ResolveSecretMaterial(context.Background(), resolver, "p1", Resolved{
		ProviderCredentialRef: &credentialRef,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(material.ProviderCredential) != "provider-plain" {
		t.Fatalf("material = %+v", material)
	}
	if len(resolver.calls) != 1 || resolver.calls[0] != credentialRef {
		t.Fatalf("resolver calls = %v", resolver.calls)
	}
	values := material.Values()
	if len(values) != 1 || values[0] != "provider-plain" {
		t.Fatalf("redaction values = %v", values)
	}
}

func TestProviderCredentialNotRequestedDoesNotRequireResolver(t *testing.T) {
	material, err := ResolveSecretMaterial(context.Background(), nil, "p1", Resolved{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(material.ProviderCredential) != 0 || len(material.Values()) != 0 {
		t.Fatalf("material = %+v", material)
	}
}

func TestProviderCredentialRequestRequiresResolver(t *testing.T) {
	credentialRef := "provider-token"
	_, err := ResolveSecretMaterial(context.Background(), nil, "p1", Resolved{ProviderCredentialRef: &credentialRef}, true)
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_secret_resolver_unavailable" {
		t.Fatalf("err = %#v", err)
	}
}
