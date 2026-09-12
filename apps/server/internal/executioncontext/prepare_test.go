package executioncontext

import (
	"context"
	"testing"
)

type fakeRedactionRegistrar struct {
	registeredRun string
	registered    []string
	releases      int
}

func (f *fakeRedactionRegistrar) Register(runID string, values []string) {
	f.registeredRun = runID
	f.registered = append([]string(nil), values...)
}

func (f *fakeRedactionRegistrar) Release(string) {
	f.releases++
}

func TestPrepareBuildsOnlyRequestedExecutionSecretsAndProvenance(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	secretResolver := &fakeSecretResolver{values: map[string][]byte{
		"provider-token": []byte("provider-plain"),
	}}
	provenance := &fakeProvenanceStore{}
	redaction := &fakeRedactionRegistrar{}
	preparer, err := NewPreparer(resolver, secretResolver, provenance, redaction)
	if err != nil {
		t.Fatal(err)
	}

	prepared, err := preparer.Prepare(context.Background(), "p1", "r1", SecretRequest{
		ProviderCredentialEnv: "PROVIDER_TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Secrets["PROVIDER_TOKEN"] != "provider-plain" {
		t.Fatalf("execution secrets = %+v", prepared.Secrets)
	}
	if len(prepared.Secrets) != 1 || len(prepared.RedactionValues) != 1 {
		t.Fatalf("prepared = %+v", prepared)
	}
	if provenance.puts != 1 {
		t.Fatalf("provenance writes = %d", provenance.puts)
	}
	if redaction.registeredRun != "r1" || len(redaction.registered) != 1 || prepared.ReleaseRedaction == nil {
		t.Fatalf("redaction registration=%+v prepared=%+v", redaction, prepared)
	}
	prepared.ReleaseRedaction()
	prepared.ReleaseRedaction()
	if redaction.releases != 1 {
		t.Fatalf("redaction releases=%d, want 1", redaction.releases)
	}
}

func TestPrepareRedactsAuthorizedProviderSecretWithoutInjectingIt(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	secretResolver := &fakeSecretResolver{values: map[string][]byte{
		"provider-token": []byte("provider-plain"),
	}}
	redaction := &fakeRedactionRegistrar{}
	preparer, err := NewPreparer(resolver, secretResolver, &fakeProvenanceStore{}, redaction)
	if err != nil {
		t.Fatal(err)
	}

	prepared, err := preparer.Prepare(context.Background(), "p1", "r1", SecretRequest{RedactAuthorizedSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Secrets) != 0 {
		t.Fatalf("attach must not inject secrets: %+v", prepared.Secrets)
	}
	if len(prepared.RedactionValues) != 1 || prepared.RedactionValues[0] != "provider-plain" {
		t.Fatalf("redaction values=%v", prepared.RedactionValues)
	}
}

func TestPrepareRejectsInvalidProviderCredentialTargetBeforeProvenance(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	secretResolver := &fakeSecretResolver{values: map[string][]byte{"provider-token": []byte("provider-plain")}}
	provenance := &fakeProvenanceStore{}
	preparer, err := NewPreparer(resolver, secretResolver, provenance)
	if err != nil {
		t.Fatal(err)
	}

	_, err = preparer.Prepare(context.Background(), "p1", "r1", SecretRequest{ProviderCredentialEnv: "BAD-NAME"})
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_secret_target_invalid" {
		t.Fatalf("err = %#v", err)
	}
	if len(secretResolver.calls) != 0 {
		t.Fatalf("secret resolver calls = %v", secretResolver.calls)
	}
	if provenance.puts != 0 {
		t.Fatal("provenance persisted for failed preflight")
	}
}
