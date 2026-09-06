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

func TestUnauthorizedRuntimeSecretFailsBeforeResolution(t *testing.T) {
	resolver := &fakeSecretResolver{values: map[string][]byte{"forged": []byte("do-not-read")}}
	_, err := ResolveSecretMaterial(context.Background(), resolver, "p1", Resolved{AllowedSecretRefs: []string{"allowed"}}, []string{"forged"})
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_secret_unauthorized" {
		t.Fatalf("err = %#v", err)
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("secret resolver called for unauthorized ref: %v", resolver.calls)
	}
}

func TestAuthorizedSecretsResolveAfterPolicyCheck(t *testing.T) {
	credentialRef := "provider-token"
	resolver := &fakeSecretResolver{values: map[string][]byte{
		"provider-token": []byte("provider-plain"),
		"runtime-token":  []byte("runtime-plain"),
	}}
	material, err := ResolveSecretMaterial(context.Background(), resolver, "p1", Resolved{
		ProviderCredentialRef: &credentialRef,
		AllowedSecretRefs:     []string{"runtime-token"},
	}, []string{"runtime-token", "runtime-token"})
	if err != nil {
		t.Fatal(err)
	}
	if string(material.ProviderCredential) != "provider-plain" || string(material.Runtime["runtime-token"]) != "runtime-plain" {
		t.Fatalf("material = %+v", material)
	}
	if len(resolver.calls) != 2 {
		t.Fatalf("resolver calls = %v", resolver.calls)
	}
	values := material.Values()
	if len(values) != 2 {
		t.Fatalf("redaction values = %v", values)
	}
}
